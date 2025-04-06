- distributed blockchain
    - will allow for privacy and immutability
- each transaction is an encrypted delta of the database
    - contains the hash of the new version of the database
    - rather than an encrypted delta, maybe use an encrypted SQL command
- agreed-upon trusted public key system 
    - master public/private keypair 
    - to become a trusted party, you must have a keypair that has been signed by the master keypair
    - assumed to be controlled by a master trusted party (Certificate Authority)
- underlying backend is probably just a SQL database
    - every node will have a copy of this database locally and update it incrementally
- new nodes must have a key established with a CA
    - follow a basic gossip protocol to find nearby nodes and authenticate with them  
    - present certificates to each neighbor accordingly; in this simplified model every node will know every other node 
    - each node has an internal database that stores the public keys of other authenticated nodes
    - Protocol: 
        - use DNS to find hardcoded root node
        - hardcoded root node provides list of local nodes 
    - implementing a full gossip protocol might be too much for the scope of the project; basic version maybe just uses hardcoded domain names and assumes the nodes to be DNS'd properly? 
        - for example, nodes are DNS'd to each other as 'node1', 'node2', 'node3'
- Need a message passing protocol for P2P communications
    -> Maybe just build atop HTTPS instead of reinventing the wheel? 
    -> Most likely will appropriate some https client/server library to do P2P message passing
    -> [example here](https://youngkin.github.io/post/gohttpsclientserver/)
- General Flow: 
    - Node joins the network (through some gossip mechanism)
    - Exchanges certificates with each neighbor node 
    - Each joining node then consults 

- Problems: how do we differentiate between read/write privileges? 
    -> each network node also has a frontend that deals with queries? 
    -> each decentralized network node needs to service broadcasts + deal with authentication from frontend requests
    -> how to properly do confidentiality? 
        |-> record system needs to be encrypted
        |-> encryption key derived by some combination of the nodes in the network?


---
- Chain will store an audit log of all nodes that join the network
- Whenever a peer joins the network, they publish their distributed identity with an associated public key 
    - uses kademlia DHT for peer discovery; probably wanna run our own [bootstrap node](https://github.com/primihub/simple-bootstrap-node/tree/develop)

- every node keeps a list of distributed identity to public key mappings 
- once a node is established they may update the audit log with new information
    - adding another DID to their access control list 
    - adding a record, modifying a record, etc. 
    - each modification is signed with their private key, and thus verification means checking merkle tree consistency + verifying the block signature 
- nodes can request the distributed identities of other nodes (majority vote on responses)
- nodes can also request the location of a peer from its distributed identity; then they can communicate with each other directly to share records (respecting ACLs)












