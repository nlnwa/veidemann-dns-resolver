package archivingcache

import (
	"context"
	"fmt"
	contentwriterV1 "github.com/nlnwa/veidemann-api-go/contentwriter/v1"
	"google.golang.org/grpc"
	r "gopkg.in/gorethink/gorethink.v4"
	"time"
)

// Connection holds the connections for ContentWriter and Veidemann database
type Connection struct {
	dbConnectOpts           r.ConnectOpts
	dbSession               r.QueryExecutor
	contentWriterAddr       string
	contentWriterClient     contentwriterV1.ContentWriterClient
	contentWriterClientConn *grpc.ClientConn
}

// NewConnection creates a new Connection object
func NewConnection(dbHost string, dbPort int, dbUser string, dbPassword string, dbName string, contentWriterHost string,
	contentWriterPort int) *Connection {
	c := &Connection{
		dbConnectOpts: r.ConnectOpts{
			Address:    fmt.Sprintf("%s:%d", dbHost, dbPort),
			Username:   dbUser,
			Password:   dbPassword,
			Database:   dbName,
			NumRetries: 10,
		},
		contentWriterAddr: fmt.Sprintf("%s:%d", contentWriterHost, contentWriterPort),
	}
	return c
}

// connect establishes connections
func (c *Connection) connect() error {
	// Set up ContentWriterClient
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure(), grpc.WithBlock())

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dialCancel()
	clientConn, err := grpc.DialContext(dialCtx, c.contentWriterAddr, opts...)
	if err != nil {
		log.Errorf("fail to dial: %v", err)
		return err
	}
	c.contentWriterClientConn = clientConn
	c.contentWriterClient = contentwriterV1.NewContentWriterClient(clientConn)

	// Set up database connection
	if c.dbConnectOpts.Database == "mock" {
		c.dbSession = r.NewMock(c.dbConnectOpts)
	} else {
		dbSession, err := r.Connect(c.dbConnectOpts)
		if err != nil {
			log.Errorf("fail to connect to database: %v", err)
			return err
		}
		c.dbSession = dbSession
	}

	log.Infof("Archiver is using contentwriter at: %s, and DB at: %s", c.contentWriterAddr, c.dbConnectOpts.Address)

	return nil
}
