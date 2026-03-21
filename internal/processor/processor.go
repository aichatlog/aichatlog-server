package processor

import (
	"context"
	"log"
	"time"

	"github.com/aichatlog/aichatlog/server/internal/output"
	"github.com/aichatlog/aichatlog/server/internal/storage"
)

// Processor is a background worker that processes received conversations
// through the pipeline: fetch → render → push via output adapter → update status.
// Optionally runs LLM extraction after syncing.
type Processor struct {
	store     *storage.Store
	adapter   output.Adapter
	extractor *Extractor
	syncDir   string
	interval  time.Duration
	batch     int
}

// Config for the processor.
type Config struct {
	// Interval between processing cycles. Default: 30s.
	Interval time.Duration
	// BatchSize is the max conversations to process per cycle. Default: 20.
	BatchSize int
	// SyncDir is the base directory in the output path. Default: "aichatlog".
	SyncDir string
}

// New creates a new Processor. If adapter is nil, processing is disabled.
// extractor may be nil to disable LLM extraction.
func New(store *storage.Store, adapter output.Adapter, extractor *Extractor, cfg *Config) *Processor {
	interval := 30 * time.Second
	batch := 20
	syncDir := "aichatlog"

	if cfg != nil {
		if cfg.Interval > 0 {
			interval = cfg.Interval
		}
		if cfg.BatchSize > 0 {
			batch = cfg.BatchSize
		}
		if cfg.SyncDir != "" {
			syncDir = cfg.SyncDir
		}
	}

	return &Processor{
		store:     store,
		adapter:   adapter,
		extractor: extractor,
		syncDir:   syncDir,
		interval:  interval,
		batch:     batch,
	}
}

// Run starts the processing loop. Blocks until ctx is cancelled.
func (p *Processor) Run(ctx context.Context) {
	if p.adapter == nil {
		log.Printf("processor: no output adapter configured, processing disabled")
		return
	}

	log.Printf("processor: started (adapter=%s, interval=%s, batch=%d)",
		p.adapter.Name(), p.interval, p.batch)

	// Process once immediately on startup
	p.cycle()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("processor: stopped")
			return
		case <-ticker.C:
			p.cycle()
		}
	}
}

// cycle processes one batch of received conversations.
func (p *Processor) cycle() {
	ids, err := p.store.ListByStatus("received", p.batch)
	if err != nil {
		log.Printf("processor: error listing received: %v", err)
		return
	}
	if len(ids) == 0 {
		return
	}

	log.Printf("processor: processing %d conversations", len(ids))

	for _, id := range ids {
		if err := p.processOne(id); err != nil {
			log.Printf("processor: error processing %s: %v", id, err)
			p.store.UpdateStatus(id, "failed")
		}
	}
}

// processOne handles a single conversation: fetch → render → push → mark synced.
func (p *Processor) processOne(id string) error {
	// 1. Fetch conversation + messages
	conv, err := p.store.Get(id)
	if err != nil || conv == nil {
		return err
	}

	messages, err := p.store.GetMessages(id)
	if err != nil {
		return err
	}

	// 2. Auto-tag (basic classification, no LLM)
	if conv.HasCode {
		p.store.AddTag(id, "has-code", true)
	}
	if conv.Project != "" {
		p.store.AddTag(id, "project:"+conv.Project, true)
	}

	// 3. Render markdown
	content, err := RenderMarkdown(conv, messages)
	if err != nil {
		return err
	}

	// 4. Push via output adapter
	path := NotePath(conv, p.syncDir)
	if err := p.adapter.Push(path, content); err != nil {
		return err
	}

	// 5. Record sync and mark as synced
	p.store.RecordSync(id, p.adapter.Name(), path, conv.ContentHash)
	p.store.UpdateStatus(id, "synced")

	log.Printf("processor: synced %s → %s", id, path)

	// 6. Optional: LLM knowledge extraction
	if p.extractor != nil {
		if err := p.extractor.ExtractOne(id); err != nil {
			log.Printf("processor: extraction error for %s: %v", id, err)
			// Don't fail the sync — extraction is optional
		}
	}

	return nil
}
