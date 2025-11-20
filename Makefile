install: grpc/auction_grpc.pb.go grpc/auction.pb.go node.exe client.exe

grpc/auction_grpc.pb.go grpc/auction.pb.go: grpc/auction.proto
	protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative grpc/auction.proto

proto: grpc/auction_grpc.pb.go grpc/auction.pb.go

client.exe: client/client.go
	go build -o client.exe client/client.go

node.exe: server/node.go
	go build -o node.exe server/node.go