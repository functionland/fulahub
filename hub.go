package fulahub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/announce"
	"github.com/ipni/go-libipni/dagsync/ipnisync"
	"github.com/ipni/go-libipni/ingest/schema"
	gostream "github.com/libp2p/go-libp2p-gostream"
	"github.com/multiformats/go-multihash"
	"github.com/multiformats/go-varint"
)

const FulaHubProtocolV0_0_1 = "/fx.land/hub/0.0.1"

var (
	log            = logging.Logger("fula/hub")
	ipniMetadata   = varint.ToUvarint(0xfe001)
	latestAdCidKey = datastore.NewKey("fula/hub/latestAdCid")
)

type (
	PutContentRequest struct {
		Mutlihashes []multihash.Multihash `json:"Mutlihashes"`
	}
	Hub struct {
		*options
		httpServer *http.Server
		publisher  *ipnisync.Publisher
		mu         sync.Mutex
	}
)

func NewHub(o ...Option) (*Hub, error) {

	opts, err := newOptions(o...)
	if err != nil {
		return nil, err
	}
	p := &Hub{
		options:    opts,
		httpServer: &http.Server{},
	}
	key := p.h.Peerstore().PrivKey(p.h.ID())

	p.publisher, err = ipnisync.NewPublisher(*p.ls, key, ipnisync.WithStreamHost(p.h),
		ipnisync.WithHeadTopic("/indexer/ingest/mainnet"))
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Hub) Start(_ context.Context) error {

	listener, err := gostream.Listen(p.h, FulaHubProtocolV0_0_1)
	if err != nil {
		return err
	}
	go func() {
		http.HandleFunc("/content", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut {
				http.Error(w, "unknown method", http.StatusBadRequest)
				return
			}

			defer r.Body.Close()
			var request PutContentRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, "failed to decode body", http.StatusBadRequest)
				return
			}

			if err := p.publish(r.Context(), []byte(r.RemoteAddr), request.Mutlihashes); err != nil {
				http.Error(w, "failed to publish: "+err.Error(), http.StatusInternalServerError)
				return
			}
		})
		if err := p.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorw("Failed to start HTTP server", "err", err)
		}
	}()
	return nil
}

func (p *Hub) publish(ctx context.Context, contextID []byte, mhs []multihash.Multihash) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	chunk, err := schema.EntryChunk{
		Entries: mhs,
	}.ToNode()
	if err != nil {
		log.Errorw("Failed to instantiate entries chunk node", "err", err)
		return err
	}
	chunkLink, err := p.ls.Store(ipld.LinkContext{Ctx: ctx}, schema.Linkproto, chunk)
	if err != nil {
		log.Errorw("Failed to store chunk in IPNI provider engine", "err", err)
		return err
	}
	ad := schema.Advertisement{
		Provider:  p.h.ID().String(),
		Addresses: p.ipniProviderAddrs(p.h),
		Entries:   chunkLink,
		ContextID: contextID,
		Metadata:  ipniMetadata,
	}
	latest, err := p.getLatestAdCid(ctx)
	if err != nil {
		return err
	}
	if latest != cid.Undef {
		ad.PreviousID = cidlink.Link{Cid: latest}
	}
	if err := ad.Sign(p.h.Peerstore().PrivKey(p.h.ID())); err != nil {
		log.Errorw("Failed to sign IPNI advertisement", "err", err)
		return err
	}
	if err := ad.Validate(); err != nil {
		return err
	}
	adNode, err := ad.ToNode()
	if err != nil {
		return err
	}
	adLink, err := p.ls.Store(ipld.LinkContext{Ctx: ctx}, schema.Linkproto, adNode)
	if err != nil {
		return err
	}
	adCid := adLink.(cidlink.Link).Cid
	p.publisher.SetRoot(adCid)
	if err := p.setLatestAdCid(ctx, adCid); err != nil {
		return err
	}
	if p.ipniAnnounceSender != nil && p.ipniAnnounceAddrs != nil {
		err := announce.Send(ctx, adCid, p.ipniAnnounceAddrs(p.h), p.ipniAnnounceSender)
		if err != nil {
			log.Errorw("Failed to announce advertisement", "ad", adLink.String())
		}
	}

	log.Infow("Successfully published ad to IPNI", "ad", adLink.String(), "entriesCount", len(mhs))
	return nil
}

func (p *Hub) getLatestAdCid(ctx context.Context) (cid.Cid, error) {
	switch v, err := p.ds.Get(ctx, latestAdCidKey); {
	case err == nil:
		_, c, err := cid.CidFromBytes(v)
		return c, err
	case errors.Is(err, datastore.ErrNotFound):
		return cid.Undef, nil
	default:
		return cid.Undef, err
	}
}

func (p *Hub) setLatestAdCid(ctx context.Context, c cid.Cid) error {
	return p.ds.Put(ctx, latestAdCidKey, c.Bytes())
}

func (p *Hub) Shutdown(ctx context.Context) error {
	hErr := p.httpServer.Shutdown(ctx)
	eErr := p.publisher.Close()
	return errors.Join(hErr, eErr)
}
