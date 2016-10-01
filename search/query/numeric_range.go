//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package query

import (
	"fmt"

	"github.com/blevesearch/bleve/index"
	"github.com/blevesearch/bleve/mapping"
	"github.com/blevesearch/bleve/search"
	"github.com/blevesearch/bleve/search/searcher"
)

type NumericRangeQuery struct {
	Min          *float64 `json:"min,omitempty"`
	Max          *float64 `json:"max,omitempty"`
	InclusiveMin *bool    `json:"inclusive_min,omitempty"`
	InclusiveMax *bool    `json:"inclusive_max,omitempty"`
	Field        string   `json:"field,omitempty"`
	Boost        *Boost   `json:"boost,omitempty"`
}

// NewNumericRangeQuery creates a new Query for ranges
// of numeric values.
// Either, but not both endpoints can be nil.
// The minimum value is inclusive.
// The maximum value is exclusive.
func NewNumericRangeQuery(min, max *float64) *NumericRangeQuery {
	return NewNumericRangeInclusiveQuery(min, max, nil, nil)
}

// NewNumericRangeInclusiveQuery creates a new Query for ranges
// of numeric values.
// Either, but not both endpoints can be nil.
// Control endpoint inclusion with inclusiveMin, inclusiveMax.
func NewNumericRangeInclusiveQuery(min, max *float64, minInclusive, maxInclusive *bool) *NumericRangeQuery {
	return &NumericRangeQuery{
		Min:          min,
		Max:          max,
		InclusiveMin: minInclusive,
		InclusiveMax: maxInclusive,
	}
}

func (q *NumericRangeQuery) SetBoost(b float64) {
	boost := Boost(b)
	q.Boost = &boost
}

func (q *NumericRangeQuery) SetField(f string) {
	q.Field = f
}

func (q *NumericRangeQuery) Searcher(i index.IndexReader, m mapping.IndexMapping, explain bool) (search.Searcher, error) {
	field := q.Field
	if q.Field == "" {
		field = m.DefaultSearchField()
	}
	return searchers.NewNumericRangeSearcher(i, q.Min, q.Max, q.InclusiveMin, q.InclusiveMax, field, q.Boost.Value(), explain)
}

func (q *NumericRangeQuery) Validate() error {
	if q.Min == nil && q.Min == q.Max {
		return fmt.Errorf("numeric range query must specify min or max")
	}
	return nil
}
