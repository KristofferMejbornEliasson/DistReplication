# DistReplication

Implementation of distributed replication using active replication.

There are three programmes; `client`, `frontent`, and (replica manager) `node`.
Clients and nodes interact with the singular frontend which forwards requests/responses.

## Compile

A Makefile is included to make compilation easier. If you have some implementation
of Make installed, you can compile the programmes by simply executing:

```
make
```

Alternatively, execute _each_ of the following three commands:

```
go build -o client.exe client/client.go
```

```
go build -o node.exe server/node.go server/auction.go
```

```
go build -o frontend.exe server/frontend.go server/auction.go
```

## Run

### Frontend
To start the frontend, execute
```
./frontend
```

### Replica manager backend
To start the replica managers, execute
```
./node <port>
```

where `<port>` is 5000, 5001, 5002, or 5003.
Note that you should always have one node with the port 5000, as this is
the primary replica manager.

To close a replica manager, input `quit` or `exit` into the terminal, or kill the
process with Ctrl+C.\
To start a new auction in the primary replica manager, type in `start`.\
To see the state of the current/latest auction, type in `state` or `auction`.

### Client
While the frontend is running (and ideally a replica manager, too),
a client programme can be run by executing either
```
./client bid <amount>
```
or
```
./client result
```






