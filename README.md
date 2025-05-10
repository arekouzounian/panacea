# panacea
*[pan-uh-see-uh]*


A **secure**, **decentralized** record sharing system inspired by blockchain concepts.

## Overview
Panacea began as a semester-long project for one of my graduate courses at USC. The goal was to create a secure audit log. 

Panacea can be thought of as a very simple overlay network with decentralized filesharing capabilities. Each 'node' is tied to a cryptographic identity, and uses *libp2p* for its underlying communication/authentication with peers. 

## Architecture
There are four main components in a panacea node:

1. A *libp2p* client, which consults other peers and broadcasts messages via `pubsub` 
2. A *libp2p* server, which services broadcasts and maintains a Kademlia-based DHT for continual peer discovery/maintenance
3. An HTTP client, which serves as a basic UI and handles user input
4. An HTTP server, which packages user actions on the frontend as actual blocks/transactions, and does processing with the blocks. 

Peers communicate with each other via `libp2p`, and broadcast new blocks in the blockchain via `pubsub` (and using protobuf for serialization). 

## Basic workflow 
Once an identity is created, a node enters the network via a hard-coded bootstrap node, and subsequently begins to discover nearby peers. A webserver instance is spun up (default port 8080), and a *very basic* HTML UI can be accessed locally. This UI allows three basic actions: 
- Adding a Name 
- Adding a Record 
- Querying a record under a given name

Names can be anything, but in order to actually be useful, they must correspond to a node's identity. Records are any file; when a record is uploaded, it's stored locally, and all peers are only notified of the record's hash. 

Suppose node A uploads file F. node A can subsequently add node B's name via the UI, authorizing B to query the record. Then, B is able to query record F under entity A, and the record is exchanged via a direct (encrypted) communication. 

Each of the above actions results in a block being added to the blockchain, with the exception of the final record exchange, which results in two blocks being added. 

The end result is a series of blocks which indicates a cryptographically verifiable sequence of updates to a distributed ledger--in other words, a secure audit log. 

## Important Notes & Details
This code is not production-ready, nor is it well-documented. This was meant as more of a proof-of-concept, and should be treated as such.

## Configuration & Execution 
The main entrypoint for a peer is in [src/p2p/peer.go](./src/p2p/peer.go). In order to successfully run several peers, one must first run one or more instances of bootstrap nodes, contained in [bootstrap/](./bootstrap/). Each of these instances has a libp2p MultiAddr that must be hard-coded into each peer in `peer.go`. 

To run the bootstrap node (from the project root): 
```sh
$ cd bootstrap 
$ go run main.go 
``` 

To run the peer (assuming bootstrap node is configured): 
```sh 
$ cd src
$ go run main.go <-p PORT> 
```
A port can optionally be specified, which is useful if running several peers locally. If not specified, it defaults to port 8080. 

## Project Structure 
Bootstrap node logic is entirely self-contained, and resides in [bootstrap](./bootstrap/).

Blockchain logic, including adding blocks and validating incoming requests, lives in the [src/chain/](./src/chain/) directory. 

[src/ledger](./src/ledger/) contains logic for updating the internal ledger state; currently, the only implementation is using a direct in-memory ledger. In theory, however, this could be extended to using any storage backend. 

[src/ledger_root](./src/ledger_root/) is a utility program designed to generate a genesis block for the blockchain; execution should not be necessary unless major changes are needed.

[src/p2p](./src/p2p/) contains the main peer logic, and is essentially where all components coalesce to provide full peer functionality.

[src/proto](./src/proto/) is where the protobuf definitions live. 

