# Ledger Root
--- 
This directory will contain utilities for bootstrapping the blockchain. 

Most importantly, generating the first block. To do this, we need to have a global keypair that is generated and subsequently distributed to every node. Then, a genesis block is created, signed with that keypair, and marshalled into a binary format to be distributed, read, and validated by each peer. 
