package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	. "DistReplication/grpc"
	. "DistReplication/time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const FrontendPort = 4999

type Client struct {
	pid       int64       // Client's process ID.
	logger    *log.Logger // Instance used for logging.
	timestamp *Lamport    // Local Lamport timestamp.
}

func main() {
	c := Client{
		pid:       int64(os.Getpid()),
		timestamp: NewLamport(),
	}

	file, err := os.OpenFile("log.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Client %d: ", c.pid)
	c.logger = log.New(file, prefix, 0)
	defer c.shutdownLogging(file)

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	targetAddress := fmt.Sprintf("localhost:%d", FrontendPort)
	conn, err := grpc.NewClient(targetAddress, opts...)
	if err != nil {
		c.logger.Fatalf("Failed to dial: %v", err)
	}
	defer func(conn *grpc.ClientConn) {
		err = conn.Close()
		if err != nil {
			c.logger.Fatalf("Error closing connection:\n%v", err)
		}
	}(conn)
	grpcClient := NewFrontendClient(conn)
	timestamp := c.timestamp.Now()

	arg := parseArguments()
	if arg == nil {
		c.logger.Fatalln("Could not parse arguments.")
	}
	switch arg.command {
	case "bid":
		c.bid(grpcClient, timestamp, arg)
	case "result":
		c.result(grpcClient, timestamp)
	}
}

func (c *Client) result(connection FrontendClient, timestamp uint64) {
	c.timestamp.Increment() // timestamp for sending request to front end.
	c.logf("Sending result request to server.")
	result, err := connection.Result(context.Background(), &Void{
		SenderID:  &c.pid,
		Timestamp: &timestamp,
	})
	if result != nil {
		c.timestamp.UpdateTime(result.GetTimestamp())
	}
	c.timestamp.Increment()

	if err != nil {
		c.fatal(err)
	}
	c.logf("Received response to result request from server.")
	if result.GetAuctionStartTime() == math.MinInt64 {
		c.fatalf("No auction is running, nor has one ever run on the server.")
	}

	c.logf("Auction started at: %v\n", time.Unix(result.GetAuctionStartTime(), 0))
	c.logf("Auction ended at: %v\n", time.Unix(result.GetAuctionEndTime(), 0))
	if result.GetLeadingID() == math.MinInt64 {
		c.logf("No-one has bid on the auction so far.")
	} else {
		c.logf("Leading bid is %d,- by %d.\n", result.GetLeadingBid(), result.GetLeadingID())
	}
}

func (c *Client) bid(connection FrontendClient, timestamp uint64, arg *Argument) {
	response, err := connection.Bid(context.Background(), &BidRequest{
		SenderID:  &c.pid,
		Timestamp: &timestamp,
		Amount:    &arg.amount,
	})
	if err != nil {
		c.fatal(err)
	}
	switch response.GetAck() {
	case EAck_Success:
		fmt.Printf("Bid successful. You are highest bidder with %d,-\n", arg.amount)
	case EAck_Fail:
		fmt.Printf("Bid failed.\n")
	default:
		fmt.Printf("Bid failed. Exception occurred.\n")
	}
}

// shutdownLogging closes the file which backs the logger.
func (c *Client) shutdownLogging(writer *os.File) {
	c.logf("Client shut down.\n")
	_ = writer.Close()
}

// logf writes a message to the log file, appending a newline if necessary.
// Mostly equivalent to log.Printf.
func (c *Client) logf(format string, v ...any) {
	prefix := fmt.Sprintf("Client %d. Time: %d. ", c.pid, c.timestamp)
	c.logger.SetPrefix(prefix)
	text := fmt.Sprintf(format, v...)
	if !(strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")) {
		text = text + "\n"
	}
	c.logger.Print(text)
	fmt.Print(text)
}

func (c *Client) log(v ...any) {
	c.logf("%v", v)
}

// fatalf writes a message to the log file (appending a newline if necessary),
// and exits the programme with exit code 1.
// Mostly equivalent to log.Fatalf
func (c *Client) fatalf(format string, v ...any) {
	c.logf(format, v...)
	os.Exit(1)
}

func (c *Client) fatal(v ...any) {
	c.fatalf("%v", v)
}

// parseArguments parses the programme arguments and returns a struct containing
// these.
//
// If the user has typed in "result" as the argument, then the Argument.amount is meaningless.
//
// Displays help text and terminates the programme with exit-code 1 if something
// goes wrong.
func parseArguments() (arg *Argument) {
	if len(os.Args) >= 2 {
		os.Args[1] = strings.ToLower(os.Args[1])
	}
	if len(os.Args) == 3 && os.Args[1] == "bid" {
		amount, err := strconv.ParseUint(os.Args[2], 10, 16)
		if err == nil {
			return &Argument{
				command: os.Args[1],
				amount:  amount,
			}
		}
	} else if len(os.Args) == 2 && os.Args[1] == "result" {
		return &Argument{
			command: os.Args[1],
		}
	}
	log.Println("Could not understand arguments.")
	fmt.Println("Usage:")
	fmt.Println("\t./client bid <amount>")
	fmt.Println("\t./client result")
	os.Exit(1)
	return nil
}

// Argument is a struct used for passing along the command-line arguments of the
// programme so that the parsing logic can be extracted from the main function.
//
// `command` contains the entire first argument string, thought of as the
// "command". Equal to either "bid" or "result".
//
// `amount` contains the amount of money which the user specified as argument to
// the command "bid". If "result" was entered, this variable is unused.
type Argument struct {
	command string
	amount  uint64
}
