package archivingcache

import (
	"github.com/miekg/dns"
	"reflect"
	"strings"
	"testing"
)

func createRequest(query string) (*dns.Msg, []byte) {
	request := new(dns.Msg)
	if query != "" {
		request.SetQuestion("example.org.", dns.TypeA)
	}
	bin, _ := request.Pack()
	bin = append([]byte{':'}, bin...)
	return request, bin
}

func collectionIdsToBin(ids []string) []byte {
	s := strings.Join(ids, ":") + ":"
	return []byte(s)
}

func TestCacheEntry_pack(t *testing.T) {
	emptyRequest, packedEmptyRequest := createRequest("")
	packedEmptyRequestWithIds := append(collectionIdsToBin([]string{"aaa", "bbb"}), packedEmptyRequest...)
	request, packedRequest := createRequest("example.org")
	packedRequestWithIds := append(collectionIdsToBin([]string{"aaa", "bbb"}), packedRequest...)

	type fields struct {
		collectionIds []string
		r             *dns.Msg
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{
		{"a", fields{collectionIds: []string{"aaa", "bbb"}, r: emptyRequest}, packedEmptyRequestWithIds, false},
		{"b", fields{collectionIds: []string{}, r: emptyRequest}, packedEmptyRequest, false},
		{"c", fields{collectionIds: []string{}, r: request}, packedRequest, false},
		{"d", fields{collectionIds: []string{"aaa", "bbb"}, r: request}, packedRequestWithIds, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := &CacheEntry{
				collectionIds: tt.fields.collectionIds,
				r:             tt.fields.r,
			}
			got, err := ce.pack()
			if (err != nil) != tt.wantErr {
				t.Errorf("CacheEntry.pack() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CacheEntry.pack() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCacheEntry_unpack(t *testing.T) {
	emptyRequest, packedEmptyRequest := createRequest("")
	packedEmptyRequestWithIds := append(collectionIdsToBin([]string{"aaa", "bbb"}), packedEmptyRequest...)
	request, packedRequest := createRequest("example.org")
	packedRequestWithIds := append(collectionIdsToBin([]string{"aaa", "bbb"}), packedRequest...)

	type args struct {
		entry []byte
	}
	tests := []struct {
		name    string
		expected  CacheEntry
		args    args
		wantErr bool
	}{
		{"a", CacheEntry{collectionIds: []string{"aaa", "bbb"}, r: emptyRequest}, args{packedEmptyRequestWithIds}, false},
		{"b", CacheEntry{collectionIds: []string{}, r: emptyRequest}, args{packedEmptyRequest}, false},
		{"c", CacheEntry{collectionIds: []string{}, r: request}, args{packedRequest}, false},
		{"d", CacheEntry{collectionIds: []string{"aaa", "bbb"}, r: request}, args{packedRequestWithIds}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := &CacheEntry{}
			if err := ce.unpack(tt.args.entry); (err != nil) != tt.wantErr {
				t.Errorf("CacheEntry.unpack() error = %v, wantErr %v", err, tt.wantErr)
			}
			if strings.Join(ce.collectionIds, ":") != strings.Join(tt.expected.collectionIds, ":") {
				t.Errorf("CacheEntry.unpack() collectionIds are not equal. Expected: %v, got: %v", tt.expected.collectionIds, ce.collectionIds)
			}
			if ce.r == nil || !reflect.DeepEqual(ce.r, tt.expected.r) {
				t.Errorf("CacheEntry.unpack() dns.Msg are not equal. Expected: %v, got: %v", tt.expected.r, ce.r)
			}
		})
	}
}

func TestCacheEntry_AddCollectionId(t *testing.T) {
	type fields struct {
		collectionIds []string
		r             *dns.Msg
	}
	type args struct {
		collectionId string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []string
	}{
		{"a", fields{collectionIds: []string{"aaa", "bbb"}, r: new(dns.Msg)}, args{"foo"}, []string{"aaa", "bbb", "foo"}},
		{"b", fields{r: new(dns.Msg)}, args{"foo"}, []string{"foo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := &CacheEntry{
				collectionIds: tt.fields.collectionIds,
				r:             tt.fields.r,
			}
			if got := ce.AddCollectionId(tt.args.collectionId); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CacheEntry.AddCollectionId() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCacheEntry_HasCollectionId(t *testing.T) {
	type fields struct {
		collectionIds []string
		r             *dns.Msg
	}
	type args struct {
		collectionId string
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{"a", fields{collectionIds: []string{"aaa", "bbb"}, r: new(dns.Msg)}, args{"foo"}, false},
		{"b", fields{r: new(dns.Msg)}, args{"foo"}, false},
		{"c", fields{r: new(dns.Msg)}, args{}, true},
		{"d", fields{collectionIds: []string{"foo"}, r: new(dns.Msg)}, args{"foo"}, true},
		{"e", fields{collectionIds: []string{"aaa", "foo"}, r: new(dns.Msg)}, args{"foo"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := &CacheEntry{
				collectionIds: tt.fields.collectionIds,
				r:             tt.fields.r,
			}
			if got := ce.HasCollectionId(tt.args.collectionId); got != tt.want {
				t.Errorf("CacheEntry.HasCollectionId() = %v, want %v", got, tt.want)
			}
		})
	}
}
