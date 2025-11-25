package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	. "DistReplication/grpc"
	"DistReplication/time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const FrontendPort = 4999

type Client struct {
	pid       int64         // Client's process ID.
	logger    *log.Logger   // Instance used for logging.
	Timestamp *time.Lamport // Local Lamport timestamp.
}

func main() {
	c := Client{pid: int64(os.Getpid())}

	file, err := os.Create("log.txt")
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Client %d: ", c.pid)
	c.logger = log.New(file, prefix, 0)
	defer c.ShutdownLogging(file)

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
	client := NewNodeClient(conn)
	timestamp := c.Timestamp.Now()

	arg := parseArguments()
	if arg == nil {
		c.logger.Fatalln("Could not parse arguments.")
	}
	if arg.command == "bid" {
		response, err := client.Bid(context.Background(), &BidRequest{
			SenderID:  &c.pid,
			Timestamp: &timestamp,
			Amount:    &arg.amount,
		})
		if err != nil {
			c.logger.Fatal(err)
		}
		switch response.GetAck() {
		case EAck_Success:
			fmt.Printf("Bid successful. You are highest bidder with %d,-\n", arg.amount)
		case EAck_Fail:
			fmt.Printf("Bid failed.\n")
		default:
			fmt.Printf("Bid failed. Exception occurred.\n")
		}
	} else if arg.command == "result" {
		result, err := client.Result(context.Background(), &Void{
			SenderID:  &c.pid,
			Timestamp: &timestamp,
		})
		if err != nil {
			c.logger.Fatal(err)
		}
		fmt.Printf("Auction started at: %d\n", result.GetAuctionStartTime())
		fmt.Printf("Auction ended at: %d\n", result.GetAuctionStartTime())
		fmt.Printf("Leading bid is %d,- by %d.\n", result.GetLeadingBid(), result.GetLeadingID())
	}
}

// ShutdownLogging closes the file which backs the logger.
func (c *Client) ShutdownLogging(writer *os.File) {
	c.logf("Client shut down.\n")
	_ = writer.Close()
}

// logf writes a message to the log file.
func (c *Client) logf(format string, v ...any) {
	prefix := fmt.Sprintf("Client %d. Time: %d. ", c.pid, c.Timestamp)
	c.logger.SetPrefix(prefix)
	text := fmt.Sprintf(format, v...)
	if !(strings.HasSuffix(format, "\n") || strings.HasSuffix(format, "\r")) {
		c.logger.Println(text)
	} else {
		c.logger.Print(text)
	}
}

// parseArguments parses the programme arguments and returns a struct containing
// these.
//
// If the user has typed in "result" as the argument, then the Argument.amount is meaningless.
//
// Displays help text and terminates the programme with exit-code 1 if something
// goes wrong.
func parseArguments() (arg *Argument) {
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
