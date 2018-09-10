package main

import (
	"context"
	"flag"
	"fmt"
	vm "github.com/nlnwa/veidemann-dns-resolver/veidemann_api"
	"google.golang.org/grpc"
	"log"
	"os"
	"time"
)

var (
	serverAddr = flag.String("server_addr", "127.0.0.1:8888", "The server address in the format of host:port")
)

func main() {

	flag.Parse()
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	conn, err := grpc.Dial(*serverAddr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()

	client := vm.NewDnsResolverClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Missing hostname(s)")
		flag.Usage()
	}

	for _, host := range args {
		start := time.Now()

		response, err := client.Resolve(ctx, &vm.ResolveRequest{Host: host, Port: 80})
		if err != nil {
			log.Fatalf("%v.Resolve(_) = _, %v: ", client, err)
		}

		log.Printf("time: %v\n", time.Since(start))

		log.Println(response)
	}
}
