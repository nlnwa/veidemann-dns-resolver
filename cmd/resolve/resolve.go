package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	dnsresolverV1 "github.com/nlnwa/veidemann-api/go/dnsresolver/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	serverAddr = flag.String("s", "127.0.0.1:8443", "The server address in the format of host:port")
)

func main() {
	flag.Parse()
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

	conn, err := grpc.NewClient(*serverAddr, opts...)
	if err != nil {
		fmt.Printf("Failed to create grpc client: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	client := dnsresolverV1.NewDnsResolverClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	args := flag.Args()
	if len(args) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "Error: Missing hostname(s)")
		flag.Usage()
	}

	for _, host := range args {
		response, err := client.Resolve(ctx, &dnsresolverV1.ResolveRequest{Host: host, Port: 80})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			break
		} else {
			fmt.Println(response.Host, response.TextualIp)
		}
	}
}
