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

	"google.golang.org/grpc"
)

const FrontendPort = 4999

type Frontend struct {
	UnimplementedFrontendServer
	logger      *log.Logger
	wait        chan struct{}
	primaryPort int64
}

func main() {
	frontend := Frontend{
		wait:        make(chan struct{}),
		primaryPort: 5000,
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
	prefix := fmt.Sprintf("Frontend: ")
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
	f.logf("Received a Bid request from client %d.", msg.GetSenderID())
	conn, err := f.createConnection()
	if err != nil {
		f.logf("Error creating connection to replica manager via port %d:\n%v",
			f.primaryPort, err)
		return nil, err
	}
	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			f.logf("Error closing connection to replica manager via port %d:\n%v",
				f.primaryPort, err)
		}
	}(conn)
	return nil, nil
}

// Result is the RPC executed when the client wants to see the result of the
// current or latest Auction.
func (f *Frontend) Result(_ context.Context, msg *Void) (*Outcome, error) {
	f.logf("Received a Result request from client %d.", msg.GetSenderID())
	conn, err := f.createConnection()
	if err != nil {
		f.logf("Error creating connection to replica manager via port %d:\n%v",
			f.primaryPort, err)
		return nil, err
	}
	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			f.logf("Error closing connection to replica manager via port %d:\n%v",
				f.primaryPort, err)
		}
	}(conn)
	return nil, nil
}

// createConnection establishes a GRPC client connection. It is the caller's
// responsibility to close it, and to check whether opening it was successful.
func (f *Frontend) createConnection() (*grpc.ClientConn, error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	targetAddress := fmt.Sprintf("localhost:%d", f.primaryPort)
	return grpc.NewClient(targetAddress, opts...)
}
