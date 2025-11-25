install: grpc/auction_grpc.pb.go grpc/auction.pb.go node.exe client.exe frontend.exe

grpc/auction_grpc.pb.go grpc/auction.pb.go: grpc/auction.proto
	protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative grpc/auction.proto

proto: grpc/auction_grpc.pb.go grpc/auction.pb.go

client.exe: client/client.go time/lamport.go
	go build -o client.exe client/client.go

node.exe: server/node.go server/auction.go time/lamport.go
	go build -o node.exe server/node.go server/auction.go

frontend.exe: server/frontend.go server/auction.go time/lamport.go
	go build -o frontend.exe server/frontend.go server/auction.go

clean:
	rm client.exe
	rm node.exe
	rm frontend.exe