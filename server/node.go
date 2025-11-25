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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	. "DistReplication/grpc"
	. "DistReplication/time"
)

type Server struct {
	UnimplementedNodeServer
	logger       *log.Logger
	wg           *sync.WaitGroup
	Timestamp    *Lamport
	Port         *int64
	Nodes        []int64
	RequestQueue []int64
	auction      *Auction
	isLeader     bool
}

func main() {
	server := Server{
		Timestamp:    new(Lamport),
		Port:         ParseArguments(os.Args),
		wg:           &sync.WaitGroup{},
		RequestQueue: make([]int64, 0),
		isLeader:     false,
	}
	server.Nodes = setupOtherNodeList(*server.Port)

	file, err := os.Create("log.txt")
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Node %d: ", *server.Port)
	server.logger = log.New(file, prefix, 0)
	defer server.ShutdownLogging(file)

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *server.Port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	RegisterNodeServer(grpcServer, &server)

	server.auction = StartNewAuction(0)
	wait := make(chan struct{})
	go server.Serve(grpcServer, lis, wait)
	go server.ReadUserInput(wait)
	for {
		select {
		case <-wait:
			return
		}
	}
}

// ReadUserInput runs constantly, reading the standard input.
// Breaks out when the user types "quit" or "exit".
func (s *Server) ReadUserInput(wait chan struct{}) {
	reader := bufio.NewScanner(os.Stdin)
	fmt.Printf("Node %d started.\n", *s.Port)
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

// ShutdownLogging closes the file which backs the logger.
func (s *Server) ShutdownLogging(writer *os.File) {
	s.logf("Node shut down.\n")
	_ = writer.Close()
}

// ParseArguments reads the command-line arguments and returns the port number specified
// therein.
func ParseArguments(args []string) *int64 {
	if len(args) != 2 {
		throwParseException("Wrong number of arguments.")
	}
	port, err := strconv.ParseInt(args[1], 10, 16)
	if err != nil {
		throwParseException("Could not parse argument as a port number.")
	}
	return &port
}

// Bid is the RPC executed when the caller wants access to the critical area.
func (s *Server) Bid(_ context.Context, msg *BidRequest) (*BidResponse, error) {
	s.logf("Received bid request from client %d.", msg.GetSenderID())
	s.Timestamp.UpdateTime(*msg.Timestamp)
	s.Timestamp.Increment()
	timestamp := s.Timestamp.Now()
	state := EAck_Exception
	if s.auction != nil && s.auction.IsActive() {
		state = EAck_Fail
		return &BidResponse{
			SenderID:  s.Port,
			Timestamp: &timestamp,
			Ack:       &state,
		}, nil
	}
	return nil, nil
}

// Result is the RPC executed when the caller is finished in the critical area,
// and found this node in its queue.
func (s *Server) Result(_ context.Context, msg *Void) (*Outcome, error) {
	s.wg.Done() // Decrements the wait-group's counter.
	s.Timestamp.UpdateTime(*msg.Timestamp)
	s.Timestamp.Increment()
	s.logf("Received reply to critical area access request from node %d.", msg.GetSenderID())
	return nil, nil
}

// exit performs the necessary actions when leaving the critical section.
func (s *Server) exit() {
	for _, targetPort := range s.RequestQueue {
		s.reply(targetPort)
	}
	s.RequestQueue = s.RequestQueue[:0]
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
	/*client := NewNodeClient(conn)
	timestamp := s.Timestamp.Now()
	_, err = client.Result(context.Background(), &Message{
		Timestamp: &timestamp,
		Id:        s.Port,
	})*/
}

// logf writes a message to the log file.
func (s *Server) logf(format string, v ...any) {
	prefix := fmt.Sprintf("Node %d. Time: %s. ", *s.Port, s.Timestamp)
	s.logger.SetPrefix(prefix)
	text := fmt.Sprintf(format, v...)
	if !(strings.HasSuffix(format, "\n") || strings.HasSuffix(format, "\r")) {
		s.logger.Println(text)
		fmt.Println(text)
	} else {
		s.logger.Print(text)
		fmt.Print(text)
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

func (s *Server) GenerateVoidMessage() *Void {
	timestamp := s.Timestamp.Now()
	return &Void{
		SenderID:  s.Port,
		Timestamp: &timestamp,
	}
}

func throwParseException(err string) {
	const UsageText string = "Usage:\n\t./node <port>"
	log.Fatalf("%s\n%s\n", err, UsageText)
}
