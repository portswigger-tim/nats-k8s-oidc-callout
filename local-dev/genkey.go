// genkey.go - Quick helper to generate NATS account keys for local testing
package main

import (
	"fmt"
	"os"

	"github.com/nats-io/nkeys"
)

func main() {
	kp, err := nkeys.CreateAccount()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create account key: %v\n", err)
		os.Exit(1)
	}

	seed, err := kp.Seed()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get seed: %v\n", err)
		os.Exit(1)
	}

	pub, err := kp.PublicKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get public key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("# NATS Account Key (for local testing)\n")
	fmt.Printf("SEED=%s\n", string(seed))
	fmt.Printf("PUB=%s\n", pub)
	fmt.Printf("\n# Write seed to signing.key:\n")
	fmt.Printf("echo '%s' > signing.key\n", string(seed))
	fmt.Printf("\n# Update nats-server.conf issuer:\n")
	fmt.Printf("sed -i.bak 's/issuer: \"AABBCCDD\"/issuer: \"%s\"/' nats-server.conf\n", pub)
}
