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

type Client struct {
	pid       int64         // Client's process ID.
	logger    *log.Logger   // Instance used for logging.
	Timestamp *time.Lamport // Local Lamport timestamp.
	Port      uint16        // The port dialed by this client.
}

func main() {
	c := Client{pid: int64(os.Getpid())}
	c.Port = parseArguments()

	file, err := os.Create("log.txt")
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Node %d: ", c.pid)
	c.logger = log.New(file, prefix, 0)
	defer c.ShutdownLogging(file)

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	targetAddress := fmt.Sprintf("localhost:%d", c.Port)
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
	var amount uint64 = 100
	_, err = client.Bid(context.Background(), &BidRequest{
		SenderId:  &c.pid,
		Timestamp: &timestamp,
		Amount:    &amount,
	})
}

// ShutdownLogging closes the file which backs the logger.
func (c *Client) ShutdownLogging(writer *os.File) {
	c.logf("Node shut down.\n")
	_ = writer.Close()
}

// logf writes a message to the log file.
func (c *Client) logf(format string, v ...any) {
	prefix := fmt.Sprintf("Node %d. Time: %d. ", c.pid, c.Timestamp)
	c.logger.SetPrefix(prefix)
	text := fmt.Sprintf(format, v...)
	if !(strings.HasSuffix(format, "\n") || strings.HasSuffix(format, "\r")) {
		c.logger.Println(text)
	} else {
		c.logger.Print(text)
	}
}

func parseArguments() (port uint16) {
	if len(os.Args) != 2 {
		log.Fatalf("Incorrect number of arguments.\nUsage: %s <port>\n", os.Args[0])
	}
	arg, err := strconv.ParseUint(os.Args[1], 10, 16)
	if err != nil {
		log.Fatalf("Invalid port number.\nUsage: %s <port>\n", os.Args[0])
	}
	return uint16(arg)
}
