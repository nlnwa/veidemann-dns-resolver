package main

import (
	"context"
	"flag"
	"fmt"
	vm "github.com/nlnwa/veidemann-api/go/dnsresolver/v1"
	"google.golang.org/grpc"
	"os"
	"time"
)

var (
	serverAddr = flag.String("s", "127.0.0.1:8443", "The server address in the format of host:port")
)

func main() {
	flag.Parse()
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	conn, err := grpc.Dial(*serverAddr, opts...)
	if err != nil {
		fmt.Printf("Failed to dial: %v:", err)
		os.Exit(1)
	}
	defer conn.Close()

	client := vm.NewDnsResolverClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	args := flag.Args()
	if len(args) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "Missing hostname(s)")
		flag.Usage()
	}

	for _, host := range args {
		start := time.Now()

		response, err := client.Resolve(ctx, &vm.ResolveRequest{Host: host, Port: 80})
		if err != nil {
			fmt.Printf("%v\n", err)
		}

		fmt.Printf("time: %v\n", time.Since(start))
		fmt.Println(response)
	}
}
