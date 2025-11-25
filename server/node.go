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
	Timestamp *Lamport
	Port      *int64
	Nodes     []int64
	auction   *Auction
	isLeader  bool
}

func main() {
	server := Server{
		Timestamp: NewLamport(),
		Port:      ParseArguments(os.Args),
		isLeader:  false,
	}
	server.Nodes = setupOtherNodeList(*server.Port)

	// Setup logging
	file, err := os.Create("log.txt")
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Node %d: ", *server.Port)
	server.logger = log.New(file, prefix, 0)
	defer server.ShutdownLogging(file)

	// Setup listening
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *server.Port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	RegisterNodeServer(grpcServer, &server)

	// Hard-codes node listening on port 5000 to be the primary replication manager
	// upon start-up.
	if *server.Port == 5000 {
		server.isLeader = true
	}
	// Starts a new auction.
	server.startAuction()

	// Listen to RPCs from frontend.
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

// Bid is the RPC executed when the frontend is forwarding a client request to
// register a new bid in the current auction.
func (s *Server) Bid(_ context.Context, msg *BidRequest) (*BidResponse, error) {
	s.logf("Received bid request from client %d.", msg.GetSenderID())
	s.Timestamp.UpdateTime(*msg.Timestamp)
	s.Timestamp.Increment()
	timestamp := s.Timestamp.Now()
	state := EAck_Exception
	if s.auction != nil {
		if s.auction.TryBid(msg.GetSenderID(), msg.GetAmount()) {
			state = EAck_Success
		} else {
			state = EAck_Fail
		}
	}
	return &BidResponse{
		SenderID:  s.Port,
		Timestamp: &timestamp,
		Ack:       &state,
	}, nil
}

// Result is the RPC executed when the client wishes to know the status of
// the current or latest auction.
func (s *Server) Result(_ context.Context, msg *Void) (*Outcome, error) {
	s.Timestamp.UpdateTime(*msg.Timestamp)
	s.Timestamp.Increment() // Timestamp for receive event from client.
	s.logf("Received Result request from client %d.", msg.GetSenderID())

	s.Timestamp.Increment() // Timestamp for send event to client.
	now := s.Timestamp.Now()
	var invalid int64 = math.MinInt64

	// No auction exists.
	if s.auction == nil {
		s.logf("No auction exists. Responding to client.")
		return &Outcome{
			SenderID:         s.Port,
			Timestamp:        &now,
			AuctionStartTime: &invalid,
			AuctionEndTime:   &invalid,
			LeadingID:        &invalid,
			LeadingBid:       nil,
		}, nil
	}

	start := s.auction.start.Unix()
	end := s.auction.end.Unix()
	leader := invalid
	if s.auction.leadingID != nil {
		leader = *s.auction.leadingID
	}
	s.logf("Responding to client with auction information.")
	return &Outcome{
		SenderID:         s.Port,
		Timestamp:        &now,
		AuctionStartTime: &start,
		AuctionEndTime:   &end,
		LeadingID:        &leader,
		LeadingBid:       s.auction.leadingBid,
	}, nil
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

// logf writes a message to the log file, appending a newline if necessary.
// Mostly equivalent to log.Printf.
func (s *Server) logf(format string, v ...any) {
	prefix := fmt.Sprintf("Replica node %d. Time: %s. ", *s.Port, s.Timestamp)
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

func (s *Server) GenerateVoidMessage() *Void {
	timestamp := s.Timestamp.Now()
	return &Void{
		SenderID:  s.Port,
		Timestamp: &timestamp,
	}
}

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

func throwParseException(err string) {
	const UsageText string = "Usage:\n\t./node <port>"
	log.Fatalf("%s\n%s\n", err, UsageText)
}
