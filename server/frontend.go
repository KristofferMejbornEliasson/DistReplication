package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"google.golang.org/grpc/credentials/insecure"

	. "DistReplication/grpc"
	. "DistReplication/time"

	"google.golang.org/grpc"
)

const FrontendPort = 4999

type Frontend struct {
	UnimplementedFrontendServer
	logger      *log.Logger
	wait        chan struct{}
	primaryPort int64
	timestamp   *Lamport
}

func main() {
	frontend := Frontend{
		wait:        make(chan struct{}),
		primaryPort: 5000,
		timestamp:   NewLamport(),
	}

	// Setup logger
	file, err := os.Create("log.txt")
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Frontend: ")
	frontend.logger = log.New(file, prefix, 0)
	defer frontend.ShutdownLogging(file)

	// Setup listening server
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", FrontendPort))
	if err != nil {
		frontend.fatalf("Failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	RegisterFrontendServer(grpcServer, &frontend)

	// Start listening
	err = grpcServer.Serve(lis)
	if err != nil {
		frontend.fatalf("Failed to serve: %v", err)
	}
	frontend.logf("Listening on %s.\n", lis.Addr())
	for {
		select {
		case <-frontend.wait:
			return
		}
	}
}

// logf writes a message to the log file, appending a newline if necessary.
// Mostly equivalent to log.Printf.
func (f *Frontend) logf(format string, v ...any) {
	prefix := fmt.Sprintf("Frontend at time %d: ", f.timestamp)
	f.logger.SetPrefix(prefix)
	text := fmt.Sprintf(format, v...)
	if !(strings.HasSuffix(format, "\n") || strings.HasSuffix(format, "\r")) {
		f.logger.Println(text)
	} else {
		f.logger.Print(text)
	}
}

// fatalf writes a message to the log file (appending a newline if necessary),
// and exits the programme with exit code 1.
// Mostly equivalent to log.Fatalf
func (f *Frontend) fatalf(format string, v ...any) {
	f.logf(format, v...)
	os.Exit(1)
}

// ShutdownLogging closes the file which backs the logger.
func (f *Frontend) ShutdownLogging(writer *os.File) {
	f.logf("Frontend shut down.\n")
	_ = writer.Close()
}

// Bid is the RPC executed when the client wants to register a new bid.
func (f *Frontend) Bid(_ context.Context, msg *BidRequest) (*BidResponse, error) {
	f.timestamp.UpdateTime(*msg.Timestamp)
	f.timestamp.Increment() // Timestamp for receive event (from client)
	f.logf("Received a Bid request from client %d.", msg.GetSenderID())

	// Connect to primary replica manager
	conn, err := f.createConnection()
	if err != nil {
		f.logf("Error creating connection to replica manager via port %d:\n%v", f.primaryPort, err)
		// TODO: Handle what to do when the primary replica manager is inaccessible.

		f.timestamp.Increment() // Timestamp for send event (to client)
		f.logf("Sending exception response to client %d.\n", msg.GetSenderID())
		ack := EAck_Exception
		return &BidResponse{
			SenderID:  msg.SenderID,
			Timestamp: msg.Timestamp,
			Ack:       &ack,
		}, err
	}
	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			f.logf("Error closing connection to replica manager via port %d:\n%v", f.primaryPort, err)
		}
	}(conn)

	// Pass on request to primary replica manager
	client := NewNodeClient(conn)
	return client.Bid(context.Background(), msg)
}

// Result is the RPC executed when the client wants to see the result of the
// current or latest Auction.
func (f *Frontend) Result(_ context.Context, msg *Void) (*Outcome, error) {
	f.timestamp.UpdateTime(*msg.Timestamp)
	f.timestamp.Increment() // Timestamp for receive event (from client)
	f.logf("Received a Result request from client %d.", msg.GetSenderID())

	// Connect to primary replica manager
	conn, err := f.createConnection()
	if err != nil {
		f.logf("Error creating connection to replica manager via port %d:\n%v", f.primaryPort, err)
		// TODO: Handle what to do when the primary replica manager is inaccessible.

		f.timestamp.Increment() // Timestamp for send event (to client)
		f.logf("Sending exception response to client %d.", msg.GetSenderID())
		return nil, err
	}
	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			f.logf("Error closing connection to replica manager via port %d:\n%v",
				f.primaryPort, err)
		}
	}(conn)

	f.logf("Connected to replica manager via port %d.\n", f.primaryPort)

	// Create client for remote procedure calls to primary replica manager
	client := NewNodeClient(conn)

	f.timestamp.Increment() // Timestamp for send event (to replica manager node)
	f.logf("Making RPC to replica manager via port %d.", f.primaryPort)

	// Execute remote procedure call on primary replica manager
	now := f.timestamp.Now()
	msg.Timestamp = &now
	outcome, err := client.Result(context.Background(), msg)
	if outcome != nil && outcome.Timestamp != nil {
		f.timestamp.UpdateTime(*outcome.Timestamp)
	}
	f.timestamp.Increment() // Timestamp for receive event (from replica manager node)

	if err != nil {
		f.logf("Error on RPC to replica manager via port %d:\n%v", f.primaryPort, err)

		f.timestamp.Increment() // Timestamp for send event (to client)
		f.logf("Sending exception response to client %d.", msg.GetSenderID())
		return outcome, err
	}
	f.logf("Received outcome from replica manager via port %d:\n%v", f.primaryPort, outcome)

	f.timestamp.Increment() // Timestamp for send event (to client)
	f.logf("Forwarding outcome back to client %d.", msg.GetSenderID())
	return outcome, err
}

// createConnection establishes a GRPC client connection. It is the caller's
// responsibility to close it, and to check whether opening it was successful.
func (f *Frontend) createConnection() (*grpc.ClientConn, error) {
	f.timestamp.Increment() // Timestamp for creating connection to replica manager.
	f.logf("Establishing connection to primary replica manager via port %d.", f.primaryPort)

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	targetAddress := fmt.Sprintf("localhost:%d", f.primaryPort)
	return grpc.NewClient(targetAddress, opts...)
}
