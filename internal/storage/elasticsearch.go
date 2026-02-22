package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"

	"github.com/Agnikulu/WikiSurge/internal/config"
	"github.com/Agnikulu/WikiSurge/internal/metrics"
	"github.com/Agnikulu/WikiSurge/internal/models"
)

// ElasticsearchClient wraps the official Elasticsearch Go client with additional functionality
type ElasticsearchClient struct {
	client        *elasticsearch.Client
	config        *config.Elasticsearch
	indexPattern  string
	bulkBuffer    chan *models.EditDocument
	bulkSize      int
	flushInterval time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
	mu            sync.Mutex
}

// BulkOperation represents a single bulk operation
type BulkOperation struct {
	Index *BulkIndex `json:"index,omitempty"`
}

type BulkIndex struct {
	Index string `json:"_index"`
	ID    string `json:"_id"`
}

// NewElasticsearchClient creates a new Elasticsearch client wrapper
func NewElasticsearchClient(cfg *config.Elasticsearch) (*ElasticsearchClient, error) {
	// Create ES client configuration
	esConfig := elasticsearch.Config{
		Addresses:     []string{cfg.URL},
		RetryOnStatus: []int{502, 503, 504, 429},
		RetryBackoff: func(i int) time.Duration {
			// Exponential backoff: 100ms, 200ms, 400ms
			return time.Duration(100*i*i) * time.Millisecond
		},
		MaxRetries:    3,
		EnableMetrics: true,
	}

	// Create client
	client, err := elasticsearch.NewClient(esConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create ES client: %w", err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := client.Ping(client.Ping.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to ping ES: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("ES ping failed with status: %s", res.Status())
	}

	esClient := &ElasticsearchClient{
		client:        client,
		config:        cfg,
		indexPattern:  "wikipedia-edits",
		bulkBuffer:    make(chan *models.EditDocument, 1000),
		bulkSize:      500,
		flushInterval: 5 * time.Second,
		stopCh:        make(chan struct{}),
	}

	// Set up ILM and index template
	if err := esClient.SetupILM(); err != nil {
		log.Printf("Warning: Failed to setup ILM: %v", err)
	}

	return esClient, nil
}

// SetupILM configures Index Lifecycle Management policy and index template
func (es *ElasticsearchClient) SetupILM() error {
	ctx := context.Background()

	// Create ILM policy
	// NOTE: No rollover action — indices use date-based naming (wikipedia-edits-YYYY-MM-DD)
	// and are created directly by the bulk indexer, not via rollover alias.
	// Only the delete phase is needed for automatic retention cleanup.
	policyName := "wikipedia-edits-policy"
	policy := map[string]interface{}{
		"policy": map[string]interface{}{
			"phases": map[string]interface{}{
				"hot": map[string]interface{}{
					"actions": map[string]interface{}{},
				},
				"delete": map[string]interface{}{
					"min_age": fmt.Sprintf("%dd", es.config.RetentionDays),
					"actions": map[string]interface{}{
						"delete": map[string]interface{}{},
					},
				},
			},
		},
	}

	policyJSON, _ := json.Marshal(policy)
	req := esapi.ILMPutLifecycleRequest{
		Policy: policyName,
		Body:   bytes.NewReader(policyJSON),
	}

	res, err := req.Do(ctx, es.client)
	if err != nil {
		return fmt.Errorf("failed to create ILM policy: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() && res.StatusCode != 400 { // 400 might mean policy already exists
		return fmt.Errorf("failed to create ILM policy, status: %s", res.Status())
	}

	// Create index template
	template := map[string]interface{}{
		"index_patterns": []string{"wikipedia-edits-*"},
		"template": map[string]interface{}{
			"settings": map[string]interface{}{
				"number_of_shards":   1,
				"number_of_replicas": 0,
				"refresh_interval":   "5s",
				"max_result_window":  10000,
				"index.lifecycle.name": policyName,
			},
			"mappings": map[string]interface{}{
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type": "keyword",
					},
					"title": map[string]interface{}{
						"type": "text",
						"fields": map[string]interface{}{
							"keyword": map[string]interface{}{
								"type": "keyword",
							},
						},
					},
					"user": map[string]interface{}{
						"type": "keyword",
					},
					"bot": map[string]interface{}{
						"type": "boolean",
					},
					"wiki": map[string]interface{}{
						"type": "keyword",
					},
					"timestamp": map[string]interface{}{
						"type":   "date",
						"format": "yyyy-MM-dd'T'HH:mm:ss.SSS'Z'",
					},
					"byte_change": map[string]interface{}{
						"type": "integer",
					},
					"comment": map[string]interface{}{
						"type": "text",
					},
					"language": map[string]interface{}{
						"type": "keyword",
					},
					"indexed_reason": map[string]interface{}{
						"type": "keyword",
					},
				},
			},
		},
	}

	templateJSON, _ := json.Marshal(template)
	templateReq := esapi.IndicesPutIndexTemplateRequest{
		Name: "wikipedia-edits",
		Body: bytes.NewReader(templateJSON),
	}

	res, err = templateReq.Do(ctx, es.client)
	if err != nil {
		return fmt.Errorf("failed to create index template: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() && res.StatusCode != 400 { // 400 might mean template already exists
		return fmt.Errorf("failed to create index template, status: %s", res.Status())
	}

	return nil
}

// IndexDocument adds a document to the bulk buffer for indexing
func (es *ElasticsearchClient) IndexDocument(doc *models.EditDocument) error {
	select {
	case es.bulkBuffer <- doc:
		return nil
	default:
		// Buffer is full, increment metric and return error
		metrics.IndexErrorsTotal.WithLabelValues().Inc()
		return fmt.Errorf("bulk buffer is full")
	}
}

// StartBulkProcessor starts the background bulk indexing processor
func (es *ElasticsearchClient) StartBulkProcessor() {
	es.wg.Add(1)
	go es.bulkProcessor()
}

// Stop gracefully stops the Elasticsearch client
func (es *ElasticsearchClient) Stop() {
	close(es.stopCh)
	es.wg.Wait()
}

// bulkProcessor is the background goroutine that handles bulk indexing
func (es *ElasticsearchClient) bulkProcessor() {
	defer es.wg.Done()

	ticker := time.NewTicker(es.flushInterval)
	defer ticker.Stop()

	batch := make([]*models.EditDocument, 0, es.bulkSize)

	for {
		select {
		case doc := <-es.bulkBuffer:
			batch = append(batch, doc)
			if len(batch) >= es.bulkSize {
				es.performBulkIndex(batch)
				batch = batch[:0] // Reset slice
			}

		case <-ticker.C:
			if len(batch) > 0 {
				es.performBulkIndex(batch)
				batch = batch[:0]
			}

		case <-es.stopCh:
			// Flush remaining documents
			if len(batch) > 0 {
				es.performBulkIndex(batch)
			}
			return
		}
	}
}

// performBulkIndex executes a bulk indexing request
func (es *ElasticsearchClient) performBulkIndex(docs []*models.EditDocument) {
	if len(docs) == 0 {
		return
	}

	start := time.Now()

	// Build bulk request body
	var buf bytes.Buffer
	for _, doc := range docs {
		indexName := es.getIndexName(doc.Timestamp)
		
		// Index operation
		meta := BulkOperation{
			Index: &BulkIndex{
				Index: indexName,
				ID:    doc.ID,
			},
		}
		metaJSON, _ := json.Marshal(meta)
		buf.Write(metaJSON)
		buf.WriteByte('\n')

		// Document
		docJSON, _ := json.Marshal(doc)
		buf.Write(docJSON)
		buf.WriteByte('\n')
	}

	// Execute bulk request
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := es.client.Bulk(
		bytes.NewReader(buf.Bytes()),
		es.client.Bulk.WithContext(ctx),
	)

	if err != nil {
		log.Printf("Bulk indexing failed: %v", err)
		metrics.IndexErrorsTotal.WithLabelValues().Add(float64(len(docs)))
		return
	}
	defer res.Body.Close()

	// Parse response
	var bulkResponse struct {
		Errors bool `json:"errors"`
		Items  []map[string]interface{} `json:"items"`
	}

	if err := json.NewDecoder(res.Body).Decode(&bulkResponse); err != nil {
		log.Printf("Failed to parse bulk response: %v", err)
		metrics.IndexErrorsTotal.WithLabelValues().Add(float64(len(docs)))
		return
	}

	// Count successes and errors
	successCount := 0
	errorCount := 0
	for _, item := range bulkResponse.Items {
		for _, op := range item {
			if opMap, ok := op.(map[string]interface{}); ok {
				if status, ok := opMap["status"].(float64); ok {
					if status < 300 {
						successCount++
					} else {
						errorCount++
						if errorMsg, ok := opMap["error"]; ok {
							log.Printf("Document indexing error: %v", errorMsg)
						}
					}
				}
			}
		}
	}

	// Update metrics
	metrics.DocsIndexedTotal.WithLabelValues().Add(float64(successCount))
	if errorCount > 0 {
		metrics.IndexErrorsTotal.WithLabelValues().Add(float64(errorCount))
	}

	// Observe latency
	duration := time.Since(start)
	metrics.ElasticsearchQueryDuration.WithLabelValues("bulk_index").Observe(duration.Seconds())

	log.Printf("Bulk indexed %d documents (%d success, %d errors) in %v", 
		len(docs), successCount, errorCount, duration)
}

// Search executes a search query
func (es *ElasticsearchClient) Search(query map[string]interface{}, indexPattern string) (map[string]interface{}, error) {
	start := time.Now()

	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := es.client.Search(
		es.client.Search.WithContext(ctx),
		es.client.Search.WithIndex(indexPattern),
		es.client.Search.WithBody(bytes.NewReader(queryJSON)),
	)

	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("search failed with status: %s", res.Status())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Observe latency
	duration := time.Since(start)
	metrics.ElasticsearchQueryDuration.WithLabelValues("search").Observe(duration.Seconds())

	return result, nil
}

// DeleteOldIndices manually deletes indices older than retention period
func (es *ElasticsearchClient) DeleteOldIndices() error {
	ctx := context.Background()

	// Calculate cutoff date
	cutoffDate := time.Now().AddDate(0, 0, -es.config.RetentionDays)
	cutoffStr := cutoffDate.Format("2006-01-02")

	// Get all indices
	res, err := es.client.Cat.Indices(
		es.client.Cat.Indices.WithContext(ctx),
		es.client.Cat.Indices.WithIndex("wikipedia-edits-*"),
		es.client.Cat.Indices.WithFormat("json"),
	)

	if err != nil {
		return fmt.Errorf("failed to list indices: %w", err)
	}
	defer res.Body.Close()

	var indices []map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&indices); err != nil {
		return fmt.Errorf("failed to decode indices response: %w", err)
	}

	// Delete old indices
	for _, index := range indices {
		indexName := index["index"].(string)
		if strings.HasPrefix(indexName, "wikipedia-edits-") {
			// Extract date from index name
			datePart := strings.TrimPrefix(indexName, "wikipedia-edits-")
			if datePart < cutoffStr {
				log.Printf("Deleting old index: %s", indexName)
				
				delRes, err := es.client.Indices.Delete([]string{indexName})
				if err != nil {
					log.Printf("Failed to delete index %s: %v", indexName, err)
					continue
				}
				delRes.Body.Close()
			}
		}
	}

	return nil
}

// getIndexName generates index name based on timestamp
func (es *ElasticsearchClient) getIndexName(timestamp time.Time) string {
	return fmt.Sprintf("%s-%s", es.indexPattern, timestamp.Format("2006-01-02"))
}

// RawClient returns the underlying elasticsearch.Client for advanced queries.
func (es *ElasticsearchClient) RawClient() *elasticsearch.Client {
	return es.client
}