package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	. "DistEx/grpc"
)

var Nodes []int64

var LamportTime *int64

var State eCriticalSystemState = RELEASED

type eCriticalSystemState uint8

const (
	RELEASED eCriticalSystemState = iota
	WANTED   eCriticalSystemState = iota
	HELD     eCriticalSystemState = iota
)

var Port *int64

var RequestQueue []int64

type Server struct {
	UnimplementedNodeServer
}

var logger *log.Logger

var wg sync.WaitGroup

func main() {
	Nodes := []int64{5000, 5001, 5002}
	_ = Nodes

	LamportTime = new(int64)
	Port = ParseArguments(os.Args)
	filename := fmt.Sprintf("log-%d.txt", *Port)
	file, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Node %d: ", *Port)
	logger = log.New(file, prefix, 0)
	defer ShutdownLogging(file)

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *Port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	nodeServer := Server{}
	RegisterNodeServer(grpcServer, nodeServer)

	wait := make(chan struct{})
	go Serve(grpcServer, lis, wait)
	go ReadUserInput(wait)
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
func ReadUserInput(wait chan struct{}) {
	reader := bufio.NewScanner(os.Stdin)
	fmt.Printf("Node %d started. Enter messages to store in shared file:\n", *Port)
	for {
		reader.Scan()
		if reader.Err() != nil {
			logger.Fatalf("failed to call Read: %v", reader.Err())
		}
		text := reader.Text()
		if text == "quit" || text == "exit" {
			wait <- struct{}{}
			break
		}
		if text == "" {
			continue
		}
		AccessCriticalResource(text)
	}
}

// Serve begins the server/service, allowing clients to execute its remote-procedure call functions.
func Serve(server *grpc.Server, lis net.Listener, wait chan struct{}) {
	err := server.Serve(lis)
	if err != nil {
		wait <- struct{}{}
		logger.Fatalf("failed to serve: %v", err)
	}
	logf("Listening on %s.\n", lis.Addr())
}

// AccessCriticalResource appends the given text string to the file called "shared.txt". */
func AccessCriticalResource(text string) {
	enter()

	file, err := os.OpenFile("shared.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Fatalf("failed to open file: %v", err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logger.Fatalf("failed to close file: %v", err)
		}
	}(file)

	stringToWrite := fmt.Sprintf("%s: T: %d; p:%d; %s\n",
		time.Now().Format(time.DateTime),
		*LamportTime,
		*Port,
		text)
	_, err = file.WriteString(stringToWrite)
	if err != nil {
		logger.Fatalf("failed to write string: %v", err)
	}
	logf("Wrote to critical resource.\n")
	exit()
}

// enter performs the necessary actions when attempting to gain access to the
// critical section.
func enter() {
	*LamportTime += 1
	State = WANTED
	for _, node := range Nodes {
		if node != *Port {
			wg.Add(1) //increment waitgroup for the routines to finish themselves
			go broadcast(node)
		}
	}

	wg.Wait() // added wait condition (meaning N-1 have been closed)
	State = HELD
}

// broadcast sends a request-for-access to the given port.
func broadcast(targetPort int64) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	targetAddress := fmt.Sprintf("localhost:%d", targetPort)
	conn, err := grpc.NewClient(targetAddress, opts...)
	if err != nil {
		logger.Fatalf("Failed to dial: %v", err)
	}
	defer func(conn *grpc.ClientConn) {
		err = conn.Close()
		if err != nil {
			logger.Fatalf("Error closing connection:\n%v", err)
		}
	}(conn)
	client := NewNodeClient(conn)
	_, err = client.Request(context.Background(), &Message{
		Timestamp: LamportTime,
		Id:        Port,
	})
	// If the client did not exist, we decrement the wait group instead of waiting
	// for a response that will never arrive.
	if err != nil {
		wg.Done()
	}
}

// ShutdownLogging closes the file which backs the logger.
func ShutdownLogging(writer *os.File) {
	logf("Server shut down.\n")
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
func Request(msg *Message) *Reply {
	isQueued := false
	if State == HELD || (State == WANTED && *LamportTime < *msg.Timestamp) {
		RequestQueue = append(RequestQueue, *msg.Id)
		isQueued = true
	}
	if *msg.Timestamp > *LamportTime {
		*LamportTime = *msg.Timestamp
		*LamportTime += 1
	}
	return &Reply{
		IsQueued: &isQueued,
	}
}

// Respond is the RPC executed when the caller is finished in the critical area.
func Respond(msg *Message) *Released {
	wg.Done()
	if *msg.Timestamp > *LamportTime {
		*LamportTime = *msg.Timestamp
	}
	return &Released{}
}

// exit performs the necessary actions when leaving the critical section.
func exit() {
	State = RELEASED
	for _, targetPort := range RequestQueue {
		reply(targetPort)
	}
}

// reply sends the individual response to a target node after this node has finished
// inside the critical section.
func reply(targetPort int64) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	targetAddress := fmt.Sprintf("localhost:%d", targetPort)
	conn, err := grpc.NewClient(targetAddress, opts...)
	if err != nil {
		logger.Fatalf("Failed to dial: %v", err)
	}
	defer func(conn *grpc.ClientConn) {
		err = conn.Close()
		if err != nil {
			logger.Fatalf("Error closing connection:\n%v", err)
		}
	}(conn)
	client := NewNodeClient(conn)
	_, err = client.Respond(context.Background(), &Message{
		Timestamp: LamportTime,
		Id:        Port,
	})
}

// logf writes a message to the log file.
func logf(format string, v ...any) {
	prefix := fmt.Sprintf("Node %d. Time: %d. ", *Port, *LamportTime)
	logger.SetPrefix(prefix)
	logger.Printf(format, v...)
}
