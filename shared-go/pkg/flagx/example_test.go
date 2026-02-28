package flagx_test

import (
	"fmt"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/flagx"
)

func ExampleNewFlagSet() {
	fs := flagx.NewFlagSet("premium", "verified", "active")
	fmt.Println("Count:", fs.Len())
	fmt.Println("Has premium:", fs.Has("premium"))
	fmt.Println("Has unknown:", fs.Has("unknown"))
	// Output:
	// Count: 3
	// Has premium: true
	// Has unknown: false
}

func ExampleFlagSet_Add() {
	fs := flagx.NewFlagSet()
	fs.Add("active")
	fs.Add("premium")
	fs.Add("active")
	fmt.Println("Count:", fs.Len())
	// Output:
	// Count: 2
}

func ExampleFlagSet_Remove() {
	fs := flagx.NewFlagSet("active", "premium", "verified")
	fs.Remove("premium")
	fs.Remove("nonexistent")
	fmt.Println("Flags:", fs.List())
	// Output:
	// Flags: [active verified]
}

func ExampleFlagSet_List() {
	fs := flagx.NewFlagSet("zebra", "alpha", "mango")
	fmt.Println("Sorted:", fs.List())
	// Output:
	// Sorted: [alpha mango zebra]
}

func ExampleFlag_Validate() {
	validFlag := flagx.Flag("premium")
	emptyFlag := flagx.Flag("")

	fmt.Println("Valid flag error:", validFlag.Validate())
	fmt.Println("Empty flag error:", emptyFlag.Validate())
	// Output:
	// Valid flag error: <nil>
	// Empty flag error: flagx: flag cannot be empty
}

func ExampleNewPostgresRepository() {
	repo, err := flagx.NewPostgresRepository(nil, "user_flags")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Println("Created repository for table:", repo != nil)
	// Output:
	// Created repository for table: true
}

func ExampleNewPostgresRepository_invalidTableName() {
	_, err := flagx.NewPostgresRepository(nil, "123-invalid")
	fmt.Println("Error:", err)
	// Output:
	// Error: flagx: invalid table name
}
