package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	. "DistReplication/grpc"

	"google.golang.org/grpc"
)

const FrontendPort = 4999

type Frontend struct {
	UnimplementedFrontendServer
	logger *log.Logger
	wait   chan struct{}
}

func main() {
	frontend := Frontend{
		wait: make(chan struct{}),
	}

	file, err := os.Create("log.txt")
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Frontend: ")
	frontend.logger = log.New(file, prefix, 0)
	defer frontend.ShutdownLogging(file)

	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", FrontendPort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	RegisterFrontendServer(grpcServer, &frontend)

	err = grpcServer.Serve(lis)
	if err != nil {
		frontend.logger.Fatalf("failed to serve: %v", err)
	}
	frontend.logf("Listening on %s.\n", lis.Addr())
	for {
		select {
		case <-frontend.wait:
			return
		}
	}
}

// logf writes a message to the log file.
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

// ShutdownLogging closes the file which backs the logger.
func (f *Frontend) ShutdownLogging(writer *os.File) {
	f.logf("Frontend shut down.\n")
	_ = writer.Close()
}

// Bid is the RPC executed when the caller wants access to the critical area.
func (f *Frontend) Bid(_ context.Context, msg *BidRequest) (*BidResponse, error) {
	f.logf("Received bid request from client %d.", msg.GetSenderID())
	return nil, nil
}

// Result is the RPC executed when the caller is finished in the critical area,
// and found this node in its queue.
func (f *Frontend) Result(_ context.Context, msg *Void) (*Outcome, error) {
	f.logf("Received reply to critical area access request from node %d.", msg.GetSenderID())
	return nil, nil
}
