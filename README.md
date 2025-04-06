# panacea
*[pan-uh-see-uh]*

## Implementation Phases
1. Core Networking Infrastructure
    - using libp2p
2. Blockchain implementation 
    - validation, consensus, chaining
    - on-chain audit log 
3. Secure Record storage 
4. Client/Server interaction 


### What does a node need to do? 
1. Establish credentials (make sure we have a DID, make sure we have a keypair, etc.)
2. Connect to the network   
    - find other nodes (peers)
    - notify peers that it has joined the network so that they are aware that it is online 
3. Sync itself with the blockchain
    - ask for updates from peers 
    - publish its record to its peers 
4. Begin making/receiving transactions