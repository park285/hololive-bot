// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
