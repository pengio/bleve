package scorch

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/blevesearch/bleve/analysis"
	"github.com/blevesearch/bleve/document"
	"github.com/blevesearch/bleve/index"
	"github.com/blevesearch/bleve/index/scorch/segment"
	"github.com/blevesearch/bleve/index/scorch/segment/mem"
	"github.com/blevesearch/bleve/index/store"
	"github.com/blevesearch/bleve/registry"
)

const Name = "scorch"

const Version uint8 = 1

type Scorch struct {
	version       uint8
	storeConfig   map[string]interface{}
	analysisQueue *index.AnalysisQueue
	stats         *Stats
	nextSegmentID uint64

	rootLock sync.RWMutex
	root     *IndexSnapshot

	closeCh       chan struct{}
	introductions chan *segmentIntroduction
}

func NewScorch(storeName string, storeConfig map[string]interface{}, analysisQueue *index.AnalysisQueue) (index.Index, error) {
	rv := &Scorch{
		version:       Version,
		storeConfig:   storeConfig,
		analysisQueue: analysisQueue,
		stats:         &Stats{},
		root:          &IndexSnapshot{},
	}
	return rv, nil
}

func (s *Scorch) Open() error {
	s.closeCh = make(chan struct{})
	s.introductions = make(chan *segmentIntroduction)
	go s.mainLoop()
	return nil
}

func (s *Scorch) Close() error {
	close(s.closeCh)
	return nil
}

func (s *Scorch) Update(doc *document.Document) error {
	b := index.NewBatch()
	b.Update(doc)
	return s.Batch(b)
}

func (s *Scorch) Delete(id string) error {
	b := index.NewBatch()
	b.Delete(id)
	return s.Batch(b)
}

// Batch applices a batch of changes to the index atomically
func (s *Scorch) Batch(batch *index.Batch) error {

	analysisStart := time.Now()

	resultChan := make(chan *index.AnalysisResult, len(batch.IndexOps))

	var numUpdates uint64
	var numPlainTextBytes uint64
	var ids []string
	for docID, doc := range batch.IndexOps {
		if doc != nil {
			// insert _id field
			doc.AddField(document.NewTextFieldCustom("_id", nil, []byte(doc.ID), document.IndexField|document.StoreField, nil))
			numUpdates++
			numPlainTextBytes += doc.NumPlainTextBytes()
		}
		ids = append(ids, docID)
	}

	// FIXME could sort ids list concurrent with analysis?

	go func() {
		for _, doc := range batch.IndexOps {
			if doc != nil {
				aw := index.NewAnalysisWork(s, doc, resultChan)
				// put the work on the queue
				s.analysisQueue.Queue(aw)
			}
		}
	}()

	// wait for analysis result
	analysisResults := make([]*index.AnalysisResult, int(numUpdates))
	// newRowsMap := make(map[string][]index.IndexRow)
	var itemsDeQueued uint64
	for itemsDeQueued < numUpdates {
		result := <-resultChan
		//newRowsMap[result.DocID] = result.Rows
		analysisResults[itemsDeQueued] = result
		itemsDeQueued++
	}
	close(resultChan)

	atomic.AddUint64(&s.stats.analysisTime, uint64(time.Since(analysisStart)))

	var newSegment segment.Segment
	if len(analysisResults) > 0 {
		newSegment = mem.NewFromAnalyzedDocs(analysisResults)
	} else {
		newSegment = mem.New()
	}
	s.prepareSegment(newSegment, ids, batch.InternalOps)

	return nil
}

func (s *Scorch) prepareSegment(newSegment segment.Segment, ids []string,
	internalOps map[string][]byte) error {

	// new introduction
	introduction := &segmentIntroduction{
		id:        atomic.AddUint64(&s.nextSegmentID, 1),
		data:      newSegment,
		ids:       ids,
		obsoletes: make(map[uint64]*roaring.Bitmap),
		internal:  internalOps,
		applied:   make(chan struct{}),
	}

	// get read lock, to optimistically prepare obsoleted info
	s.rootLock.RLock()
	for i := range s.root.segment {
		delta := s.root.segment[i].segment.DocNumbers(ids)
		introduction.obsoletes[s.root.segment[i].id] = delta
	}
	s.rootLock.RUnlock()

	s.introductions <- introduction

	// block until this segment is applied
	<-introduction.applied

	return nil
}

func (s *Scorch) SetInternal(key, val []byte) error {
	b := index.NewBatch()
	b.SetInternal(key, val)
	return s.Batch(b)
}

func (s *Scorch) DeleteInternal(key []byte) error {
	b := index.NewBatch()
	b.DeleteInternal(key)
	return s.Batch(b)
}

// Reader returns a low-level accessor on the index data. Close it to
// release associated resources.
func (s *Scorch) Reader() (index.IndexReader, error) {
	s.rootLock.RLock()
	defer s.rootLock.RUnlock()
	return &Reader{
		root: s.root,
	}, nil
}

func (s *Scorch) Stats() json.Marshaler {
	return s.stats
}
func (s *Scorch) StatsMap() map[string]interface{} {
	return s.stats.statsMap()
}

func (s *Scorch) Analyze(d *document.Document) *index.AnalysisResult {
	rv := &index.AnalysisResult{
		Document: d,
		Analyzed: make([]analysis.TokenFrequencies, len(d.Fields)+len(d.CompositeFields)),
		Length:   make([]int, len(d.Fields)+len(d.CompositeFields)),
	}

	for i, field := range d.Fields {
		if field.Options().IsIndexed() {
			fieldLength, tokenFreqs := field.Analyze()
			rv.Analyzed[i] = tokenFreqs
			rv.Length[i] = fieldLength

			if len(d.CompositeFields) > 0 {
				// see if any of the composite fields need this
				for _, compositeField := range d.CompositeFields {
					compositeField.Compose(field.Name(), fieldLength, tokenFreqs)
				}
			}
		}
	}

	return rv
}

func (s *Scorch) Advanced() (store.KVStore, error) {
	return nil, nil
}

func init() {
	registry.RegisterIndexType(Name, NewScorch)
}
