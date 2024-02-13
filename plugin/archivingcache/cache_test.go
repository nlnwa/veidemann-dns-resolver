package archivingcache

import (
	"reflect"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func createRequest(query string) (*dns.Msg, []byte) {
	request := new(dns.Msg)
	if query != "" {
		request.SetQuestion("example.org.", dns.TypeA)
	}
	bin, _ := request.Pack()
	// bin = append([]byte{':'}, bin...)
	return request, bin
}

func packAddr(proxyAddr string) []byte {
	var packedProxyAddr []byte
	packedProxyAddr = append(packedProxyAddr, proxyAddr...)
	return append(packedProxyAddr, '|')
}

func packIds(ids []string) []byte {
	if len(ids) == 0 {
		return []byte(":")
	}
	return []byte(strings.Join(ids, ":") + "::")
}

func TestCacheEntry_pack(t *testing.T) {
	proxyAddr := "127.0.0.1:53"
	collectionIds := []string{"aaa", "bbb"}
	name := "example.org"

	packedCollectionIds := packIds(collectionIds)
	packedProxyAddr := packAddr(proxyAddr)

	// empty request
	emptyRequest, packedEmptyRequest := createRequest("")
	onlyPackedEmptyRequest := append(packIds(nil), packedEmptyRequest...)
	packedAddrAndEmptyRequest := append(packedProxyAddr, onlyPackedEmptyRequest...)
	packedIdsAndEmptyRequest := append(packedCollectionIds, packedEmptyRequest...)
	packedAddrAndIdsAndEmptyRequest := append(packedProxyAddr, packedIdsAndEmptyRequest...)

	// request
	request, packedRequest := createRequest(name)
	onlyPackedRequest := append(packIds(nil), packedRequest...)
	packedAddrAndRequest := append(packedProxyAddr, onlyPackedRequest...)
	packedIdsAndRequest := append(packedCollectionIds, packedRequest...)
	packedAddrAndIdsAndRequest := append(packedProxyAddr, packedIdsAndRequest...)

	tests := []struct {
		name    string
		entry   *CacheEntry
		want    []byte
		wantErr bool
	}{
		{"a", &CacheEntry{Msg: emptyRequest}, onlyPackedEmptyRequest, false},
		{"b", &CacheEntry{ProxyAddr: proxyAddr, Msg: emptyRequest}, packedAddrAndEmptyRequest, false},
		{"c", &CacheEntry{CollectionIds: collectionIds, Msg: emptyRequest}, packedIdsAndEmptyRequest, false},
		{"d", &CacheEntry{ProxyAddr: proxyAddr, CollectionIds: collectionIds, Msg: emptyRequest}, packedAddrAndIdsAndEmptyRequest, false},
		{"e", &CacheEntry{Msg: request}, onlyPackedRequest, false},
		{"f", &CacheEntry{ProxyAddr: proxyAddr, Msg: request}, packedAddrAndRequest, false},
		{"g", &CacheEntry{CollectionIds: collectionIds, Msg: request}, packedIdsAndRequest, false},
		{"h", &CacheEntry{ProxyAddr: proxyAddr, CollectionIds: collectionIds, Msg: request}, packedAddrAndIdsAndRequest, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := CacheEntry{
				ProxyAddr:     tt.entry.ProxyAddr,
				CollectionIds: tt.entry.CollectionIds,
				Msg:           tt.entry.Msg,
			}
			got, err := ce.pack()
			if (err != nil) != tt.wantErr {
				t.Errorf("CacheEntry.args() error: %v, wantErr: %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s: got: %s, want: %s", tt.name, got, tt.want)
			}
		})
	}
}

func TestCacheEntry_unpack(t *testing.T) {
	collectionIds := []string{"aaa", "bbb"}
	addr := "127.0.0.1:53"
	name := "example.org"
	packedAddr := packAddr(addr)
	packedIds := packIds(collectionIds)

	emptyRequest, packedEmptyRequest := createRequest("")
	packedNilIdsAndEmptyRequest := append(packIds(nil), packedEmptyRequest...)
	packedAddrAndEmptyRequest := append(packedAddr, packedNilIdsAndEmptyRequest...)
	packedIdsAndEmptyRequest := append(packedIds, packedEmptyRequest...)
	packedAddrAndIdsAndEmptyRequest := append(packedAddr, packedIdsAndEmptyRequest...)

	request, packedRequest := createRequest(name)
	packedNilIdsAndRequest := append(packIds(nil), packedRequest...)
	packedAddrAndRequest := append(packedAddr, packedNilIdsAndRequest...)
	packedIdsAndRequest := append(packedIds, packedRequest...)
	packedAddrAndIdsAndRequest := append(packedAddr, packedIdsAndRequest...)

	tests := []struct {
		name    string
		want    *CacheEntry
		args    []byte
		wantErr bool
	}{
		{"a", &CacheEntry{Msg: emptyRequest}, append(packIds(nil), packedEmptyRequest...), false},
		{"b", &CacheEntry{ProxyAddr: addr, Msg: emptyRequest}, packedAddrAndEmptyRequest, false},
		{"c", &CacheEntry{CollectionIds: collectionIds, Msg: emptyRequest}, packedIdsAndEmptyRequest, false},
		{"d", &CacheEntry{ProxyAddr: addr, CollectionIds: collectionIds, Msg: emptyRequest}, packedAddrAndIdsAndEmptyRequest, false},
		{"e", &CacheEntry{Msg: request}, packedNilIdsAndRequest, false},
		{"f", &CacheEntry{ProxyAddr: addr, Msg: request}, packedAddrAndRequest, false},
		{"g", &CacheEntry{CollectionIds: collectionIds, Msg: request}, packedIdsAndRequest, false},
		{"h", &CacheEntry{ProxyAddr: addr, CollectionIds: collectionIds, Msg: request}, packedAddrAndIdsAndRequest, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := &CacheEntry{}
			if err := got.unpack(tt.args); (err != nil) != tt.wantErr {
				t.Errorf("CacheEntry.unpack() error = %v, wantErr %v", err, tt.wantErr)
			}
			if strings.Join(got.CollectionIds, ":") != strings.Join(tt.want.CollectionIds, ":") {
				t.Errorf("CacheEntry.unpack() CollectionIds are not equal. Expected: %v, got: %v", tt.want.CollectionIds, got.CollectionIds)
			}
			if got.Msg == nil || !reflect.DeepEqual(got.Msg, tt.want.Msg) {
				t.Errorf("CacheEntry.unpack() dns.Msg are not equal. Expected: %v, got: %v", tt.want.Msg, got.Msg)
			}
		})
	}
}

func TestCacheEntry_AddCollectionId(t *testing.T) {
	tests := []struct {
		name         string
		entry        *CacheEntry
		collectionId string
		want         []string
	}{
		{"a", &CacheEntry{CollectionIds: []string{"aaa", "bbb"}, Msg: new(dns.Msg)}, "foo", []string{"aaa", "bbb", "foo"}},
		{"b", &CacheEntry{Msg: new(dns.Msg)}, "foo", []string{"foo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := &CacheEntry{
				CollectionIds: tt.entry.CollectionIds,
				Msg:           tt.entry.Msg,
			}
			if got := ce.AddCollectionId(tt.collectionId); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CacheEntry.AddCollectionId() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCacheEntry_HasCollectionId(t *testing.T) {
	tests := []struct {
		name         string
		entry        *CacheEntry
		collectionId string
		want         bool
	}{
		{"a", &CacheEntry{CollectionIds: []string{"aaa", "bbb"}, Msg: new(dns.Msg)}, "foo", false},
		{"b", &CacheEntry{Msg: new(dns.Msg)}, "foo", false},
		{"c", &CacheEntry{Msg: new(dns.Msg)}, "", true},
		{"d", &CacheEntry{CollectionIds: []string{"foo"}, Msg: new(dns.Msg)}, "foo", true},
		{"e", &CacheEntry{CollectionIds: []string{"aaa", "foo"}, Msg: new(dns.Msg)}, "foo", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := &CacheEntry{
				CollectionIds: tt.entry.CollectionIds,
				Msg:           tt.entry.Msg,
			}
			if got := ce.HasCollectionId(tt.collectionId); got != tt.want {
				t.Errorf("CacheEntry.HasCollectionId() = %v, want %v", got, tt.want)
			}
		})
	}
}
