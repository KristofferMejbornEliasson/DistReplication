package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	. "DistReplication/grpc"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	pid         int64
	logger      *log.Logger
	LamportTime int64
}

func main() {
	c := Client{pid: int64(os.Getpid())}

	filename := fmt.Sprintf("log-%d.txt", c.pid)
	file, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}
	prefix := fmt.Sprintf("Node %d: ", c.pid)
	c.logger = log.New(file, prefix, 0)
	defer c.ShutdownLogging(file)

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	targetAddress := fmt.Sprintf("localhost:%d", 5000)
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
	_, err = client.Request(context.Background(), &Message{
		Timestamp: &c.LamportTime,
		Id:        &c.pid,
	})
}

// ShutdownLogging closes the file which backs the logger.
func (c *Client) ShutdownLogging(writer *os.File) {
	c.logf("Node shut down.\n")
	_ = writer.Close()
}

// logf writes a message to the log file.
func (c *Client) logf(format string, v ...any) {
	prefix := fmt.Sprintf("Node %d. Time: %d. ", c.pid, c.LamportTime)
	c.logger.SetPrefix(prefix)
	text := fmt.Sprintf(format, v...)
	if !(strings.HasSuffix(format, "\n") || strings.HasSuffix(format, "\r")) {
		c.logger.Println(text)
	} else {
		c.logger.Print(text)
	}
}
