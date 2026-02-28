package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	password := flag.String("password", "", "plain password (or VERIFY_PASSWORD env)")
	hash := flag.String("hash", "", "bcrypt hash (or VERIFY_HASH env)")
	flag.Parse()

	passwordValue := *password
	if passwordValue == "" {
		passwordValue = os.Getenv("VERIFY_PASSWORD")
	}

	hashValue := *hash
	if hashValue == "" {
		hashValue = os.Getenv("VERIFY_HASH")
	}

	if passwordValue == "" || hashValue == "" {
		fmt.Fprintln(os.Stderr, "Usage: verifyhash -password <plain> -hash <bcrypt> (or set VERIFY_PASSWORD/VERIFY_HASH)")
		os.Exit(2)
	}

	err := bcrypt.CompareHashAndPassword([]byte(hashValue), []byte(passwordValue))
	if err != nil {
		fmt.Printf("Verification FAILED: %v\n", err)
	} else {
		fmt.Println("Verification SUCCESS")
	}
}
