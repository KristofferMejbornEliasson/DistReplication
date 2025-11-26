package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/credentials/insecure"

	. "DistReplication/grpc"
	. "DistReplication/time"

	"google.golang.org/grpc"
)

const FrontendPort int64 = 4999

type Frontend struct {
	UnimplementedFrontendServer
	UnimplementedFrontendMaintenanceServer
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
	file, err := os.OpenFile("log.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Frontend: ")
	frontend.logger = log.New(file, prefix, 0)
	defer frontend.shutdownLogging(file)

	// Setup listening server
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", FrontendPort))
	if err != nil {
		frontend.fatalf("Failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	RegisterFrontendServer(grpcServer, &frontend)
	RegisterFrontendMaintenanceServer(grpcServer, &frontend)

	// Start listening
	frontend.logf("Starting listening on %s.", lis.Addr())
	err = grpcServer.Serve(lis)
	if err != nil {
		frontend.fatalf("Failed to serve: %v", err)
	}
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
	prefix := fmt.Sprintf("Frontend at time %s: ", f.timestamp)
	f.logger.SetPrefix(prefix)
	text := fmt.Sprintf(format, v...)
	if !(strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")) {
		text = text + "\n"
	}
	f.logger.Print(text)
	fmt.Print(text)
}

// fatalf writes a message to the log file (appending a newline if necessary),
// and exits the programme with exit code 1.
// Mostly equivalent to log.Fatalf
func (f *Frontend) fatalf(format string, v ...any) {
	f.logf(format, v...)
	os.Exit(1)
}

// shutdownLogging closes the file which backs the logger.
func (f *Frontend) shutdownLogging(writer *os.File) {
	f.logf("Frontend shut down.\n")
	_ = writer.Close()
}

// Bid is the RPC executed when the client wants to register a new bid.
// It passes this request onto the primary replica manager and returns the response.
func (f *Frontend) Bid(_ context.Context, msg *BidRequest) (*BidResponse, error) {
	f.timestamp.UpdateTime(*msg.Timestamp)
	f.timestamp.Increment() // timestamp for receive event (from client)
	f.logf("Received a Bid request from client %d.", msg.GetSenderID())

	f.timestamp.Increment() // timestamp for creating connection to replica manager node.

	// Connect to primary replica manager
	conn, err := f.createConnection()
	if err != nil {
		// An error here is unrelated to whether something is actually listening
		// on the given port. Nothing we can do about this.
		f.logf("Error creating connection to replica manager via port %d:\n%v", f.primaryPort, err)

		f.timestamp.Increment() // timestamp for send event (to client)
		f.logf("Sending exception response to client %d.\n", msg.GetSenderID())
		return f.generateErrorBidResponse(), err
	}
	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			f.logf("Error closing connection to replica manager via port %d:\n%v", f.primaryPort, err)
		}
	}(conn)

	f.timestamp.Increment() // timestamp for send event to replica manager node.

	// Pass on request to primary replica manager
	client := NewNodeClient(conn)
	now := f.timestamp.Now()
	requestID := uuid.New().String()
	msg.Timestamp = &now
	msg.RequestID = &requestID
	response, err := client.Bid(context.Background(), msg)
	if response != nil && response.Timestamp != nil {
		f.timestamp.UpdateTime(*response.Timestamp)
	}
	f.timestamp.Increment() // timestamp for receive event (from replica manager node)

	if err != nil || response == nil {
		f.logf("Error on RPC to replica manager via port %d:\n%v", f.primaryPort, err)
		// TODO: Handle what to do when the primary replica manager is inaccessible.

		f.timestamp.Increment() // timestamp for send event (to client)
		f.logf("Sending exception response to client %d.", msg.GetSenderID())
		return f.generateErrorBidResponse(), err
	}

	f.logf("Received outcome from replica manager via port %d:\n%v", f.primaryPort, response)

	f.timestamp.Increment() // timestamp for send event (to client)
	f.logf("Forwarding response back to client %d.", msg.GetSenderID())

	now = f.timestamp.Now()
	newResponse := &BidResponse{
		SenderID:  &f.primaryPort,
		Timestamp: &now,
		Ack:       response.Ack,
	}
	return newResponse, err
}

// Result is the RPC executed when the client wants to see the result of the
// current or latest Auction.
func (f *Frontend) Result(_ context.Context, msg *Void) (*Outcome, error) {
	f.timestamp.UpdateTime(*msg.Timestamp)
	f.timestamp.Increment() // timestamp for receive event (from client)
	f.logf("Received a Result request from client %d.", msg.GetSenderID())

	// Connect to primary replica manager
	conn, err := f.createConnection()
	if err != nil {
		// An error here is unrelated to whether something is actually listening
		// on the given port. Nothing we can do about this.
		f.logf("Error creating connection to replica manager via port %d:\n%v", f.primaryPort, err)

		f.timestamp.Increment() // timestamp for send event (to client)
		f.logf("Sending exception response to client %d.", msg.GetSenderID())
		return f.generateErrorOutcome(), err
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

	f.timestamp.Increment() // timestamp for send event (to replica manager node)
	f.logf("Making RPC to replica manager via port %d.", f.primaryPort)

	// Execute remote procedure call on primary replica manager
	now := f.timestamp.Now()
	msg.Timestamp = &now
	outcome, err := client.Result(context.Background(), msg)
	if outcome != nil && outcome.Timestamp != nil {
		f.timestamp.UpdateTime(*outcome.Timestamp)
	}
	f.timestamp.Increment() // timestamp for receive event (from replica manager node)

	if err != nil {
		f.logf("Error on RPC to replica manager via port %d:\n%v", f.primaryPort, err)
		// TODO: Handle what to do when the primary replica manager is inaccessible.

		f.timestamp.Increment() // timestamp for send event (to client)
		f.logf("Sending exception response to client %d.", msg.GetSenderID())
		return f.generateErrorOutcome(), err
	}
	f.logf("Received outcome from replica manager via port %d:\n%v", f.primaryPort, outcome)

	f.timestamp.Increment() // timestamp for send event (to client)
	f.logf("Forwarding outcome back to client %d.", msg.GetSenderID())
	return outcome, err
}

func (f *Frontend) ChangePrimary(_ context.Context, msg *Void) (*Void, error) {
	f.timestamp.UpdateTime(*msg.Timestamp)
	f.timestamp.Increment()
	f.logf("Received a UpdatePrimary request from replica manager at port %d.", msg.GetSenderID())
	f.primaryPort = msg.GetSenderID()
	f.timestamp.Increment()
	f.logf("Responding to UpdatePrimary request from replica manager at port %d.", msg.GetSenderID())
	now := f.timestamp.Now()
	this := FrontendPort
	return &Void{
		SenderID:  &this,
		Timestamp: &now,
	}, nil
}

// createConnection establishes a GRPC client connection. It is the caller's
// responsibility to close it, and to check whether opening it was successful.
func (f *Frontend) createConnection() (*grpc.ClientConn, error) {
	f.timestamp.Increment() // timestamp for creating connection to replica manager.
	f.logf("Establishing connection to primary replica manager via port %d.", f.primaryPort)

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	targetAddress := fmt.Sprintf("localhost:%d", f.primaryPort)
	return grpc.NewClient(targetAddress, opts...)
}

func (f *Frontend) generateErrorBidResponse() *BidResponse {
	now := f.timestamp.Now()
	ack := EAck_Exception
	return &BidResponse{
		SenderID:  &f.primaryPort,
		Timestamp: &now,
		Ack:       &ack,
	}
}

func (f *Frontend) generateErrorOutcome() *Outcome {
	now := f.timestamp.Now()
	var invalid int64 = math.MinInt64
	return &Outcome{
		SenderID:         &f.primaryPort,
		Timestamp:        &now,
		AuctionStartTime: &invalid,
		AuctionEndTime:   &invalid,
		LeadingBid:       nil,
		LeadingID:        &invalid,
	}
}
