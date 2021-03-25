package archivingcache

import (
	"fmt"
	contentwriterV1 "github.com/nlnwa/veidemann-api/go/contentwriter/v1"
	r "gopkg.in/rethinkdb/rethinkdb-go.v6"
	"time"
)

// ContentWriterClient holds the connections for ContentWriter and Veidemann database
type database struct {
	ConnectOpts r.ConnectOpts
	Session     r.QueryExecutor
}

// NewContentWriterClient creates a new ContentWriterClient object
func NewDbConnection(dbHost string, dbPort int, dbUser string, dbPassword string, dbName string) *database {
	c := &database{
		ConnectOpts: r.ConnectOpts{
			Address:    fmt.Sprintf("%s:%d", dbHost, dbPort),
			Username:   dbUser,
			Password:   dbPassword,
			Database:   dbName,
			NumRetries: 10,
		},
	}
	return c
}

// Connect establishes connections
func (c *database) Connect() error {
	if c.Session != nil && c.Session.IsConnected() {
		return nil
	}
	dbSession, err := r.Connect(c.ConnectOpts)
	if err != nil {
		return err
	}
	c.Session = dbSession
	return nil
}

func (c *database) Close() error {
	if s, ok := c.Session.(*r.Session); ok {
		return s.Close()
	}
	return nil
}

// WriteCrawlLog stores a crawl log of a dns request/response.
func (c *database) WriteCrawlLog(payload []byte, record *contentwriterV1.WriteResponseMeta_RecordMeta, requestedHost string, fetchStart time.Time, fetchDurationMs int64, proxyAddr string) error {
	crawlLog := map[string]interface{}{
		"recordType":          "resource",
		"requestedUri":        "dns:" + requestedHost,
		"discoveryPath":       "P",
		"statusCode":          1,
		"timeStamp":           time.Now().UTC(),
		"fetchTimeStamp":      fetchStart,
		"fetchTimeMs":         fetchDurationMs,
		"ipAddress":           proxyAddr,
		"contentType":         "text/dns",
		"size":                int64(len(payload)),
		"warcId":              record.GetWarcId(),
		"blockDigest":         record.GetBlockDigest(),
		"payloadDigest":       record.GetPayloadDigest(),
		"collectionFinalName": record.GetCollectionFinalName(),
		"storageRef":          record.GetStorageRef(),
	}

	_, err := r.Table("crawl_log").Insert(crawlLog).RunWrite(c.Session)
	if err != nil {
		return err
	}
	return nil
}
