package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	. "DistReplication/grpc"
	. "DistReplication/time"
)

type Server struct {
	UnimplementedNodeServer
	logger    *log.Logger
	timestamp *Lamport
	port      *int64
	nodes     []int64
	auction   *Auction
	isLeader  bool
}

func main() {
	server := Server{
		timestamp: NewLamport(),
		port:      parseArguments(os.Args),
		isLeader:  false,
	}
	server.nodes = setupOtherNodeList(*server.port)

	// Setup logging
	file, err := os.OpenFile("log.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Node %d: ", *server.port)
	server.logger = log.New(file, prefix, 0)
	defer server.shutdownLogging(file)

	// Setup listening
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *server.port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	RegisterNodeServer(grpcServer, &server)

	// Hard-codes node listening on port 5000 to be the primary replication manager
	// upon start-up.
	if *server.port == 5000 {
		server.isLeader = true
	}
	// Starts a new auction.
	server.startAuction()

	// Listen to RPCs from frontend.
	wait := make(chan struct{})
	go server.serve(grpcServer, lis, wait)
	go server.readUserInput(wait)
	for {
		select {
		case <-wait:
			return
		}
	}
}

// readUserInput runs constantly, reading the standard input.
// Breaks out when the user types "quit" or "exit".
// Type 'start' to start a new 100-second auction if one isn't active.
func (s *Server) readUserInput(wait chan struct{}) {
	reader := bufio.NewScanner(os.Stdin)
	fmt.Printf("Node %d started.\n", *s.port)
	s.logf("Node has begun listening to user input through standard input.")
	for {
		reader.Scan()
		if reader.Err() != nil {
			s.logger.Fatalf("failed to call Read: %v", reader.Err())
		}
		text := reader.Text()
		text = strings.ToLower(text)
		if text == "" {
			continue
		}
		if text == "quit" || text == "exit" {
			wait <- struct{}{}
			break
		}
		if text == "start" {
			s.startAuction()
		}
		if text == "state" || text == "auction" {
			s.printAuction()
		}
	}
}

// serve begins the server/service, allowing clients to execute its remote-procedure call functions.
func (s *Server) serve(server *grpc.Server, lis net.Listener, wait chan struct{}) {
	err := server.Serve(lis)
	if err != nil {
		wait <- struct{}{}
		s.logger.Fatalf("failed to serve: %v", err)
	}
	s.logf("Listening on %s.\n", lis.Addr())
}

// shutdownLogging closes the file which backs the logger.
func (s *Server) shutdownLogging(writer *os.File) {
	s.logf("Node shut down.\n")
	_ = writer.Close()
}

// parseArguments reads the command-line arguments and returns the port number specified
// therein.
func parseArguments(args []string) *int64 {
	if len(args) != 2 {
		throwParseException("Wrong number of arguments.")
	}
	port, err := strconv.ParseInt(args[1], 10, 16)
	if err != nil {
		throwParseException("Could not parse argument as a port number.")
	}
	return &port
}

func throwParseException(err string) {
	const UsageText string = "Usage:\n\t./node <port>"
	log.Fatalf("%s\n%s\n", err, UsageText)
}

// Bid is the RPC executed when the frontend is forwarding a client request to
// register a new bid in the current auction.
func (s *Server) Bid(_ context.Context, msg *BidRequest) (*BidResponse, error) {
	s.logf("Received bid request from client %d.", msg.GetSenderID())
	s.timestamp.UpdateTime(*msg.Timestamp)
	s.timestamp.Increment() // timestamp for receive event from frontend
	timestamp := s.timestamp.Now()
	state := EAck_Exception
	if s.auction != nil {
		if s.auction.TryBid(msg.GetSenderID(), msg.GetAmount()) {
			state = EAck_Success
			s.updateBackups(msg.RequestID)
		} else {
			state = EAck_Fail
		}
	}

	s.timestamp.Increment() // timestamp for send event to frontend
	timestamp = s.timestamp.Now()
	return &BidResponse{
		SenderID:  s.port,
		Timestamp: &timestamp,
		Ack:       &state,
	}, nil
}

// Result is the RPC executed when the client wishes to know the status of
// the current or latest auction.
func (s *Server) Result(_ context.Context, msg *Void) (*Outcome, error) {
	s.timestamp.UpdateTime(*msg.Timestamp)
	s.timestamp.Increment() // timestamp for receive event from frontend.
	s.logf("Received Result request from client %d.", msg.GetSenderID())

	s.timestamp.Increment() // timestamp for send event to frontend.
	if s.auction == nil {
		s.logf("No auction exists. Responding to client.")
	} else {
		s.logf("Responding to client with auction information.")
	}
	return s.generateOutcome(), nil
}

// updateBackups executes the Update RPC on each other replica manager in Server.nodes.
// This is a synchronous operation.
func (s *Server) updateBackups(requestID *string) {
	outcome := s.generateOutcome()
	for _, port := range s.nodes {
		s.updateBackup(port, outcome, requestID)
	}
}

// updateBackup sends the state of the current Auction to the backup at the given port.
func (s *Server) updateBackup(targetPort int64, outcome *Outcome, requestID *string) {
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
	s.timestamp.Increment()
	timestamp := s.timestamp.Now()
	outcome.Timestamp = &timestamp
	response, err := client.Update(context.Background(), &UpdateQuery{
		Outcome:   outcome,
		RequestID: requestID,
	})
	if err != nil {
		s.timestamp.Increment()
		s.logf("Could not update backup at port %d: %v", targetPort, err)
	} else {
		s.timestamp.UpdateTime(*response.Timestamp)
		s.timestamp.Increment()
		s.logf("Updated backup at port %d.", targetPort)
	}
}

// logf writes a message to the log file, appending a newline if necessary.
// Mostly equivalent to log.Printf.
func (s *Server) logf(format string, v ...any) {
	prefix := fmt.Sprintf("Replica node %d. Time: %s. ", *s.port, s.timestamp)
	s.logger.SetPrefix(prefix)
	text := fmt.Sprintf(format, v...)
	if !(strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")) {
		text = text + "\n"
	}
	s.logger.Print(text)
	fmt.Print(text)
}

// fatalf writes a message to the log file (appending a newline if necessary),
// and exits the programme with exit code 1.
// Mostly equivalent to log.Fatalf
func (s *Server) fatalf(format string, v ...any) {
	s.logf(format, v...)
	os.Exit(1)
}

// setupOtherNodeList creates the list of other distributed nodes.
func setupOtherNodeList(port int64) []int64 {
	nodes := []int64{5000, 5001, 5002, 5003}

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

// generateVoidMessage creates a Void struct, which contains information on the
// sender and their current Lamport timestamp.
func (s *Server) generateVoidMessage() *Void {
	timestamp := s.timestamp.Now()
	return &Void{
		SenderID:  s.port,
		Timestamp: &timestamp,
	}
}

// startAuction creates a new Auction with a start time when it is called, and
// an end-time 100 seconds later.
func (s *Server) startAuction() {
	if s.isLeader {
		if s.auction == nil || s.auction.end.Before(time.Now()) {
			s.auction = StartNewAuction(0)
			s.logf("Started a new auction from %v to %v, with a starting bid of %d DKK.\n",
				s.auction.start, s.auction.end, s.auction.leadingBid)
		} else {
			s.logf("Failed to start a new auction. One is already in progress.")
		}
	} else {
		s.logf("Failed to start a new auction. This node is not the leader.")
	}
}

// Update is the RPC executed in a backup when the leader wishes to update it
// with changes to the Auction state.
func (s *Server) Update(_ context.Context, msg *UpdateQuery) (*Void, error) {
	outcome := msg.GetOutcome()
	s.timestamp.UpdateTime(outcome.GetTimestamp())
	s.timestamp.Increment() // timestamp for receiving event
	s.logf("Received Update call from replica manager at port %d.", outcome.GetSenderID())

	s.auction = Reconstruct(
		outcome.LeadingBid,
		outcome.LeadingID,
		outcome.GetAuctionStartTime(),
		outcome.GetAuctionEndTime())

	s.timestamp.Increment() // timestamp for send event
	s.logf("Responding to update from replica mananger at port %d.", outcome.GetSenderID())
	return s.generateVoidMessage(), nil
}

// generateOutcome generates an Outcome struct which contains information on the
// current state of the Auction.
func (s *Server) generateOutcome() *Outcome {
	now := s.timestamp.Now()
	var invalid int64 = math.MinInt64

	// No auction exists.
	if s.auction == nil {
		return &Outcome{
			SenderID:         s.port,
			Timestamp:        &now,
			AuctionStartTime: &invalid,
			AuctionEndTime:   &invalid,
			LeadingID:        &invalid,
			LeadingBid:       nil,
		}
	}

	start := s.auction.start.Unix()
	end := s.auction.end.Unix()
	leader := invalid
	if s.auction.leadingID != nil {
		leader = *s.auction.leadingID
	}
	return &Outcome{
		SenderID:         s.port,
		Timestamp:        &now,
		AuctionStartTime: &start,
		AuctionEndTime:   &end,
		LeadingID:        &leader,
		LeadingBid:       s.auction.leadingBid,
	}
}

func (s *Server) printAuction() {
	s.logf("Local command executed to print state to standard output:\n")
	if s.auction == nil {
		s.logf("No auction is running, nor has one ever run on the server.\n")
		return
	}

	s.logf("Auction started at: %v\n", s.auction.start)
	s.logf("Auction ended at: %v\n", s.auction.end)
	if s.auction.leadingID == nil || *s.auction.leadingID == math.MinInt64 {
		s.logf("No-one has bid on the auction so far.")
	} else {
		s.logf("Leading bid is %d,- by %d.\n", *s.auction.leadingBid, *s.auction.leadingID)
	}
}
