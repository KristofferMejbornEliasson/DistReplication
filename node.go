package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	. "DistEx/grpc"
)

type eCriticalSystemState uint8

const (
	RELEASED eCriticalSystemState = iota
	WANTED   eCriticalSystemState = iota
	HELD     eCriticalSystemState = iota
)

var RequestQueue []int64

type Server struct {
	UnimplementedNodeServer
	logger      *log.Logger
	wg          *sync.WaitGroup
	LamportTime *int64
	Port        *int64
	TimeLock    *sync.Mutex
	Nodes       []int64
	State       eCriticalSystemState
}

var Service Server

func main() {
	Service := Server{
		LamportTime: new(int64),
		Port:        ParseArguments(os.Args),
		wg:          &sync.WaitGroup{},
		TimeLock:    new(sync.Mutex),
		State:       RELEASED,
	}
	Service.Nodes = setupOtherNodeList(*Service.Port)

	filename := fmt.Sprintf("log-%d.txt", *Service.Port)
	file, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Node %d: ", *Service.Port)
	Service.logger = log.New(file, prefix, 0)
	defer Service.ShutdownLogging(file)

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *Service.Port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	RegisterNodeServer(grpcServer, &Service)

	wait := make(chan struct{})
	go Service.Serve(grpcServer, lis, wait)
	go Service.ReadUserInput(wait)
	for {
		select {
		case <-wait:
			return
		}
	}
}

// ReadUserInput runs constantly, reading the standard input.
// Breaks out when the user types "quit" or "exit", and ignores empty lines.
// Otherwise, calls [AccessCriticalResource].
func (s *Server) ReadUserInput(wait chan struct{}) {
	reader := bufio.NewScanner(os.Stdin)
	fmt.Printf("Node %d started. Enter messages to store in shared file:\n", *s.Port)
	s.logf("Node has begun listening to user input through standard input.")
	for {
		reader.Scan()
		if reader.Err() != nil {
			s.logger.Fatalf("failed to call Read: %v", reader.Err())
		}
		text := reader.Text()
		if text == "quit" || text == "exit" {
			wait <- struct{}{}
			break
		}
		if text == "" {
			continue
		}
		s.AccessCriticalResource(text)
	}
}

// Serve begins the server/service, allowing clients to execute its remote-procedure call functions.
func (s *Server) Serve(server *grpc.Server, lis net.Listener, wait chan struct{}) {
	err := server.Serve(lis)
	if err != nil {
		wait <- struct{}{}
		s.logger.Fatalf("failed to serve: %v", err)
	}
	s.logf("Listening on %s.\n", lis.Addr())
}

// AccessCriticalResource appends the given text string to the file called "shared.txt". */
func (s *Server) AccessCriticalResource(text string) {
	s.enter()

	file, err := os.OpenFile("shared.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		s.logger.Fatalf("failed to open file: %v", err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			s.logger.Fatalf("failed to close file: %v", err)
		}
	}(file)

	stringToWrite := fmt.Sprintf("%s: T: %d; p:%d; %s\n",
		time.Now().Format(time.DateTime),
		*s.LamportTime,
		*s.Port,
		text)
	_, err = file.WriteString(stringToWrite)
	if err != nil {
		s.logger.Fatalf("failed to write string: %v", err)
	}
	s.logf("Wrote to critical resource: \"%s\"\n", text)
	s.exit()
}

// enter performs the necessary actions when attempting to gain access to the
// critical section.
func (s *Server) enter() {
	s.IncrementTime()
	s.State = WANTED
	s.logf("Setting state to WANTED.")
	s.logf("Broadcasting to all other nodes.")
	for _, node := range s.Nodes {
		s.logf("Broadcast to node %d\n", node)
		s.wg.Add(1) // Increments the wait-group's counter.
		go s.broadcast(node)
	}

	s.wg.Wait() // Blocks until the wait-group's counter is 0.
	s.State = HELD
	s.logf("Received N-1 replies. Setting state to HELD.")
}

// broadcast sends a request-for-access to the given port.
func (s *Server) broadcast(targetPort int64) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	targetAddress := fmt.Sprintf("localhost:%d", targetPort)
	conn, err := grpc.NewClient(targetAddress, opts...)
	if err != nil {
		s.logger.Fatalf("Failed to dial: %v", err)
	}
	defer func(conn *grpc.ClientConn) {
		err = conn.Close()
		if err != nil {
			s.logger.Fatalf("Error closing connection:\n%v", err)
		}
	}(conn)
	client := NewNodeClient(conn)
	_, err = client.Request(context.Background(), &Message{
		Timestamp: s.LamportTime,
		Id:        s.Port,
	})
	// If the client did not exist, we decrement the wait group instead of waiting
	// for a response that will never arrive.
	if err != nil {
		s.wg.Done() // Decrements the wait-group's counter.
		s.logf("Failed to broadcast to node %d. Skipping it.", targetPort)
	} else {
		s.logf("Broadcasted to node %d.", targetPort)
	}
}

// ShutdownLogging closes the file which backs the logger.
func (s *Server) ShutdownLogging(writer *os.File) {
	s.logf("Node shut down.\n")
	_ = writer.Close()
}

// ParseArguments reads the command-line arguments and returns the port number specified
// therein.
func ParseArguments(args []string) *int64 {
	if len(args) != 2 {
		log.Fatal("Usage: go run main.go <port>")
	}
	port, err := strconv.ParseInt(args[1], 10, 16)
	if err != nil {
		log.Fatal(err)
	}
	return &port
}

// Request is the RPC executed when the caller wants access to the critical area.
func (s *Server) Request(ctx context.Context, msg *Message) (*Reply, error) {
	isBusy := false
	s.logf("Received critical area access request from node %d.", msg.GetId())
	if s.State == HELD || (s.State == WANTED && *s.LamportTime < *msg.Timestamp) {
		isBusy = true
	}
	s.UpdateTime(msg.Timestamp)
	s.IncrementTime()
	if isBusy {
		s.logf("Adding request to queue.")
		RequestQueue = append(RequestQueue, msg.GetId())
	} else {
		s.logf("Replying immediately to request.")
		s.reply(msg.GetId())
	}
	return &Reply{
		IsQueued: &isBusy,
	}, nil
}

// Respond is the RPC executed when the caller is finished in the critical area,
// and found this node in its queue.
func (s *Server) Respond(ctx context.Context, msg *Message) (*Released, error) {
	s.wg.Done() // Decrements the wait-group's counter.
	s.UpdateTime(msg.Timestamp)
	s.IncrementTime()
	s.logf("Received reply to critical area access request from node %d.", msg.GetId())
	return &Released{}, nil
}

// exit performs the necessary actions when leaving the critical section.
func (s *Server) exit() {
	s.State = RELEASED
	for _, targetPort := range RequestQueue {
		s.reply(targetPort)
	}
}

// reply sends the individual response to a target node after this node has finished
// inside the critical section.
func (s *Server) reply(targetPort int64) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	targetAddress := fmt.Sprintf("localhost:%d", targetPort)
	conn, err := grpc.NewClient(targetAddress, opts...)
	if err != nil {
		s.logger.Fatalf("Failed to dial: %v", err)
	}
	defer func(conn *grpc.ClientConn) {
		err = conn.Close()
		if err != nil {
			s.logger.Fatalf("Error closing connection:\n%v", err)
		}
	}(conn)
	client := NewNodeClient(conn)
	_, err = client.Respond(context.Background(), &Message{
		Timestamp: s.LamportTime,
		Id:        s.Port,
	})
}

// logf writes a message to the log file.
func (s *Server) logf(format string, v ...any) {
	prefix := fmt.Sprintf("Node %d. Time: %d. ", *s.Port, *s.LamportTime)
	s.logger.SetPrefix(prefix)
	text := fmt.Sprintf(format, v...)
	if !(strings.HasSuffix(format, "\n") || strings.HasSuffix(format, "\r")) {
		s.logger.Println(text)
	} else {
		s.logger.Print(text)
	}
}

// setupOtherNodeList creates the list of other distributed nodes.
func setupOtherNodeList(port int64) []int64 {
	nodes := []int64{5000, 5001, 5002}

	// Remove own node from list
	for i := range nodes {
		if nodes[i] == port {
			if i == len(nodes)-1 {
				nodes = nodes[:i]
			} else {
				nodes = append(nodes[:i], nodes[i+1:]...)
			}
			break
		}
	}
	return nodes
}

func (s *Server) IncrementTime() {
	s.TimeLock.Lock()
	defer s.TimeLock.Unlock()
	*s.LamportTime += 1
}

func (s *Server) UpdateTime(other *int64) {
	s.TimeLock.Lock()
	defer s.TimeLock.Unlock()
	if *s.LamportTime < *other {
		*s.LamportTime = *other
	}
}
