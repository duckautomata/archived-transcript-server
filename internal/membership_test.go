package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

/**
These tests are used to verify everything about membership is working as expected.
There might be some overlap with other tests, but that's ok as those are testing for general functionality, not membership.
*/

// seedTestData inserts the required transcripts for testing.
func seedTestData(t *testing.T, app *App) {
	ctx := context.Background()

	transcripts := []TranscriptInput{
		{
			ID:            "eeb65mIOpfs",
			Streamer:      "TestStreamer",
			Date:          "2023-01-01",
			StreamType:    "Members",
			StreamTitle:   "TestStreamer Members Stream",
			SrtTranscript: "1\n00:00:01,000 --> 00:00:04,000\nthe secret content\n\n",
		},
		{
			ID:            "5jvNKUzrI4Q",
			Streamer:      "TestStreamer",
			Date:          "2023-01-02",
			StreamType:    "Stream",
			StreamTitle:   "TestStreamer Public Stream",
			SrtTranscript: "1\n00:00:01,000 --> 00:00:04,000\nthe public content\n\n",
		},
		{
			ID:            "eI8e0eDfmQs",
			Streamer:      "OtherStreamer",
			Date:          "2023-01-03",
			StreamType:    "Members",
			StreamTitle:   "OtherStreamer Members Stream",
			SrtTranscript: "1\n00:00:01,000 --> 00:00:04,000\nthe other secret\n\n",
		},
		{
			ID:            "IVcjM0mQD64",
			Streamer:      "OtherStreamer",
			Date:          "2023-01-04",
			StreamType:    "Stream",
			StreamTitle:   "OtherStreamer Public Stream",
			SrtTranscript: "1\n00:00:01,000 --> 00:00:04,000\nthe other public\n\n",
		},
	}

	for _, tr := range transcripts {
		if err := app.insertTranscript(ctx, &tr); err != nil {
			t.Fatalf("Failed to insert transcript %s: %v", tr.ID, err)
		}
	}
}

func TestMembershipVerification(t *testing.T) {
	app := setupTestApp(t)
	seedTestData(t, app)
	defer app.db.Close()

	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()
	host := ts.URL

	// Helper to make requests
	checkStatus := func(url, key string, expectedStatus int) {
		req, _ := http.NewRequest("GET", url, nil)
		if key != "" {
			req.Header.Set("X-Membership-Key", key)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != expectedStatus {
			t.Errorf("GET %s (key=%s): expected status %d, got %d", url, key, expectedStatus, resp.StatusCode)
		}
	}

	checkGraphData := func(url, key string, expectedCount int) {
		req, _ := http.NewRequest("GET", url, nil)
		if key != "" {
			req.Header.Set("X-Membership-Key", key)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		var output GraphOutput
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			t.Fatalf("Failed to decode graph response: %v", err)
		}

		if len(output.Result) != expectedCount {
			t.Errorf("GET %s (key=%s): expected %d results, got %d", url, key, expectedCount, len(output.Result))
		}
	}

	checkQueryData := func(url, key string, expectedCount int) {
		req, _ := http.NewRequest("GET", url, nil)
		if key != "" {
			req.Header.Set("X-Membership-Key", key)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		var output TranscriptSearchOutput
		if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
			t.Fatalf("Failed to decode query response: %v", err)
		}

		if len(output.Result) != expectedCount {
			t.Errorf("GET %s (key=%s): expected %d results, got %d", url, key, expectedCount, len(output.Result))
		}
	}

	// --- 1. Access (No Key) ---
	t.Log("[1] Testing Access (No Key)")

	// Stream Info
	checkStatus(host+"/stream/eeb65mIOpfs", "", http.StatusNotFound) // TestStreamer Members
	checkStatus(host+"/stream/5jvNKUzrI4Q", "", http.StatusOK)       // TestStreamer Public
	checkStatus(host+"/stream/eI8e0eDfmQs", "", http.StatusNotFound) // OtherStreamer Members
	checkStatus(host+"/stream/IVcjM0mQD64", "", http.StatusOK)       // OtherStreamer Public

	// Transcripts
	checkStatus(host+"/transcript/eeb65mIOpfs", "", http.StatusNotFound) // TestStreamer Members
	checkStatus(host+"/transcript/5jvNKUzrI4Q", "", http.StatusOK)       // TestStreamer Public
	checkStatus(host+"/transcript/eI8e0eDfmQs", "", http.StatusNotFound) // OtherStreamer Members
	checkStatus(host+"/transcript/IVcjM0mQD64", "", http.StatusOK)       // OtherStreamer Public

	// Graph
	checkGraphData(host+"/graph/eeb65mIOpfs?searchText=the", "", 0) // TestStreamer Members
	checkGraphData(host+"/graph/5jvNKUzrI4Q?searchText=the", "", 1) // TestStreamer Public
	checkGraphData(host+"/graph/eI8e0eDfmQs?searchText=the", "", 0) // OtherStreamer Members
	checkGraphData(host+"/graph/IVcjM0mQD64?searchText=the", "", 1) // OtherStreamer Public

	// Graph All
	checkGraphData(host+"/graph?searchText=the&streamer=TestStreamer&streamType=Members", "", 0)
	checkGraphData(host+"/graph?searchText=the&streamer=TestStreamer&streamType=Stream", "", 1)
	checkGraphData(host+"/graph?searchText=the&streamer=OtherStreamer&streamType=Members", "", 0)
	checkGraphData(host+"/graph?searchText=the&streamer=OtherStreamer&streamType=Stream", "", 1)

	// Graph All Vague
	checkGraphData(host+"/graph?searchText=the", "", 2)                        // 2 because there are 2 public transcripts
	checkGraphData(host+"/graph?searchText=the&streamer=TestStreamer", "", 1)  // 1 because there is only 1 public transcript for TestStreamer
	checkGraphData(host+"/graph?searchText=the&streamer=OtherStreamer", "", 1) // 1 because there is only 1 public transcript for OtherStreamer

	// Query
	checkQueryData(host+"/transcripts?searchText=the&streamer=TestStreamer&streamType=Members", "", 0)
	checkQueryData(host+"/transcripts?searchText=the&streamer=TestStreamer&streamType=Stream", "", 1)
	checkQueryData(host+"/transcripts?searchText=the&streamer=OtherStreamer&streamType=Members", "", 0)
	checkQueryData(host+"/transcripts?searchText=the&streamer=OtherStreamer&streamType=Stream", "", 1)

	// Query Vague
	checkQueryData(host+"/transcripts?searchText=the", "", 2)                        // 2 because there are 2 public transcripts
	checkQueryData(host+"/transcripts?searchText=the&streamer=TestStreamer", "", 1)  // 1 because there is only 1 public transcript for TestStreamer
	checkQueryData(host+"/transcripts?searchText=the&streamer=OtherStreamer", "", 1) // 1 because there is only 1 public transcript for OtherStreamer

	// --- 2. Create Key for TestStreamer ---
	t.Log("[2] Creating Key for TestStreamer...")
	createReq, _ := http.NewRequest("POST", host+"/membership/TestStreamer", nil)
	createReq.Header.Set("X-API-Key", "456")
	createResp, err := client.Do(createReq)
	if err != nil {
		t.Fatalf("Create key failed: %v", err)
	}
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("Create key status: %d", createResp.StatusCode)
	}
	var keyData map[string]string
	json.NewDecoder(createResp.Body).Decode(&keyData)
	createResp.Body.Close()

	testKey := keyData["key"]
	if testKey == "" {
		t.Fatal("Created key was empty")
	}

	// --- 3. Verify Key ---
	t.Log("[3] Verifying Key...")
	verifyReq, _ := http.NewRequest("GET", host+"/membership/verify", nil)
	verifyReq.Header.Set("X-Membership-Key", testKey)
	verifyResp, err := client.Do(verifyReq)
	if err != nil {
		t.Fatalf("Verify request failed: %v", err)
	}
	defer verifyResp.Body.Close()
	if verifyResp.StatusCode != http.StatusOK {
		t.Errorf("Verify failed status: %d", verifyResp.StatusCode)
	}
	var verifyData map[string]string
	json.NewDecoder(verifyResp.Body).Decode(&verifyData)
	if verifyData["channel"] != "TestStreamer" {
		t.Errorf("Verify channel mismatch: got %s, want TestStreamer", verifyData["channel"])
	}

	// --- 4. Testing Access with Key ---
	t.Log("[4] Testing Access with TestStreamer Key")

	// Transcripts
	checkStatus(host+"/stream/eeb65mIOpfs", testKey, http.StatusOK)       // TestStreamer Members (Allowed)
	checkStatus(host+"/stream/5jvNKUzrI4Q", testKey, http.StatusOK)       // TestStreamer Public (Allowed)
	checkStatus(host+"/stream/eI8e0eDfmQs", testKey, http.StatusNotFound) // OtherStreamer Members (Denied) - Cross Channel
	checkStatus(host+"/stream/IVcjM0mQD64", testKey, http.StatusOK)       // OtherStreamer Public (Allowed)

	// Transcripts
	checkStatus(host+"/transcript/eeb65mIOpfs", testKey, http.StatusOK)       // TestStreamer Members (Allowed)
	checkStatus(host+"/transcript/5jvNKUzrI4Q", testKey, http.StatusOK)       // TestStreamer Public (Allowed)
	checkStatus(host+"/transcript/eI8e0eDfmQs", testKey, http.StatusNotFound) // OtherStreamer Members (Denied) - Cross Channel
	checkStatus(host+"/transcript/IVcjM0mQD64", testKey, http.StatusOK)       // OtherStreamer Public (Allowed)

	// Graph
	checkGraphData(host+"/graph/eeb65mIOpfs?searchText=the", testKey, 1) // TestStreamer Members
	checkGraphData(host+"/graph/5jvNKUzrI4Q?searchText=the", testKey, 1) // TestStreamer Public
	checkGraphData(host+"/graph/eI8e0eDfmQs?searchText=the", testKey, 0) // OtherStreamer Members (Denied)
	checkGraphData(host+"/graph/IVcjM0mQD64?searchText=the", testKey, 1) // OtherStreamer Public

	// Graph All
	checkGraphData(host+"/graph?searchText=the&streamer=TestStreamer&streamType=Members", testKey, 1)
	checkGraphData(host+"/graph?searchText=the&streamer=TestStreamer&streamType=Stream", testKey, 1)
	checkGraphData(host+"/graph?searchText=the&streamer=OtherStreamer&streamType=Members", testKey, 0)
	checkGraphData(host+"/graph?searchText=the&streamer=OtherStreamer&streamType=Stream", testKey, 1)

	// Graph All Vague
	checkGraphData(host+"/graph?searchText=the", testKey, 3)
	checkGraphData(host+"/graph?searchText=the&streamer=TestStreamer", testKey, 2)
	checkGraphData(host+"/graph?searchText=the&streamer=OtherStreamer", testKey, 1)

	// Query
	checkQueryData(host+"/transcripts?searchText=the&streamer=TestStreamer&streamType=Members", testKey, 1) // Allowed
	checkQueryData(host+"/transcripts?searchText=the&streamer=TestStreamer&streamType=Stream", testKey, 1)
	checkQueryData(host+"/transcripts?searchText=the&streamer=OtherStreamer&streamType=Members", testKey, 0)
	checkQueryData(host+"/transcripts?searchText=the&streamer=OtherStreamer&streamType=Stream", testKey, 1)

	// Query Vague
	checkQueryData(host+"/transcripts?searchText=the", testKey, 3)
	checkQueryData(host+"/transcripts?searchText=the&streamer=TestStreamer", testKey, 2)
	checkQueryData(host+"/transcripts?searchText=the&streamer=OtherStreamer", testKey, 1)

	// --- 5. List Keys ---
	t.Log("[5] Listing keys...")
	listReq, _ := http.NewRequest("GET", host+"/membership", nil)
	listReq.Header.Set("X-API-Key", "456")
	listResp, err := client.Do(listReq)
	if err != nil {
		t.Fatalf("List keys failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Errorf("Incorrect status code: Got %d, want %d", listResp.StatusCode, http.StatusOK)
	}
	var listData map[string][]string
	json.NewDecoder(listResp.Body).Decode(&listData)
	if len(listData) != 1 {
		t.Errorf("Incorrect number of keys: Got %d, want %d", len(listData), 1)
	}

	// --- 6. Delete Keys ---
	t.Log("[6] Deleting keys...")
	delReq, _ := http.NewRequest("DELETE", host+"/membership/TestStreamer", nil)
	delReq.Header.Set("X-API-Key", "456")
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("Delete keys failed: %v", err)
	}
	defer delResp.Body.Close()
	var delData map[string][]string
	json.NewDecoder(delResp.Body).Decode(&delData)
	if len(delData) != 0 {
		t.Errorf("Incorrect number of keys: Got %d, want %d", len(delData), 0)
	}

	// --- 7. Verify Keys were Deleted ---
	t.Log("[7] Verifying keys were deleted...")
	verifyReq, _ = http.NewRequest("GET", host+"/membership/TestStreamer", nil)
	verifyReq.Header.Set("X-API-Key", "456")
	verifyResp, err = client.Do(verifyReq)
	if err != nil {
		t.Fatalf("Verify request failed: %v", err)
	}
	defer verifyResp.Body.Close()
	if verifyResp.StatusCode != http.StatusOK {
		t.Errorf("Incorrect status code: Got %d, want %d", verifyResp.StatusCode, http.StatusOK)
	}

	// --- 8. Access with Expired/Deleted Key ---
	t.Log("[8] Testing Access with deleted key")

	// Should behave like unauthorized
	checkStatus(host+"/stream/eeb65mIOpfs", testKey, http.StatusNotFound) // TestStreamer Members (Denied)
	checkStatus(host+"/stream/5jvNKUzrI4Q", testKey, http.StatusOK)       // TestStreamer Public (Allowed)
	checkStatus(host+"/stream/eI8e0eDfmQs", testKey, http.StatusNotFound) // OtherStreamer Members (Denied) - Cross Channel
	checkStatus(host+"/stream/IVcjM0mQD64", testKey, http.StatusOK)       // OtherStreamer Public (Allowed)

	// Transcript
	checkStatus(host+"/transcript/eeb65mIOpfs", testKey, http.StatusNotFound) // TestStreamer Members (Denied)
	checkStatus(host+"/transcript/5jvNKUzrI4Q", testKey, http.StatusOK)       // TestStreamer Public (Allowed)
	checkStatus(host+"/transcript/eI8e0eDfmQs", testKey, http.StatusNotFound) // OtherStreamer Members (Denied) - Cross Channel
	checkStatus(host+"/transcript/IVcjM0mQD64", testKey, http.StatusOK)       // OtherStreamer Public (Allowed)

	// Graph
	checkGraphData(host+"/graph/eeb65mIOpfs?searchText=the", testKey, 0) // TestStreamer Members (Denied)
	checkGraphData(host+"/graph/5jvNKUzrI4Q?searchText=the", testKey, 1) // TestStreamer Public
	checkGraphData(host+"/graph/eI8e0eDfmQs?searchText=the", testKey, 0) // OtherStreamer Members (Denied)
	checkGraphData(host+"/graph/IVcjM0mQD64?searchText=the", testKey, 1) // OtherStreamer Public

	// Graph All
	checkGraphData(host+"/graph?searchText=the&streamer=TestStreamer&streamType=Members", testKey, 0)
	checkGraphData(host+"/graph?searchText=the&streamer=TestStreamer&streamType=Stream", testKey, 1)
	checkGraphData(host+"/graph?searchText=the&streamer=OtherStreamer&streamType=Members", testKey, 0)
	checkGraphData(host+"/graph?searchText=the&streamer=OtherStreamer&streamType=Stream", testKey, 1)

	// Graph All Vague
	checkGraphData(host+"/graph?searchText=the", testKey, 2)
	checkGraphData(host+"/graph?searchText=the&streamer=TestStreamer", testKey, 1)
	checkGraphData(host+"/graph?searchText=the&streamer=OtherStreamer", testKey, 1)

	// Query
	checkQueryData(host+"/transcripts?searchText=the&streamer=TestStreamer&streamType=Members", testKey, 0) // Denied
	checkQueryData(host+"/transcripts?searchText=the&streamer=TestStreamer&streamType=Stream", testKey, 1)
	checkQueryData(host+"/transcripts?searchText=the&streamer=OtherStreamer&streamType=Members", testKey, 0)
	checkQueryData(host+"/transcripts?searchText=the&streamer=OtherStreamer&streamType=Stream", testKey, 1)

	// Query Vague
	checkQueryData(host+"/transcripts?searchText=the", testKey, 2)
	checkQueryData(host+"/transcripts?searchText=the&streamer=TestStreamer", testKey, 1)
	checkQueryData(host+"/transcripts?searchText=the&streamer=OtherStreamer", testKey, 1)
}

func TestMembershipKeyRotation(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()

	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()
	host := ts.URL

	createKey := func() string {
		req, _ := http.NewRequest("POST", host+"/membership/TestStreamer", nil)
		req.Header.Set("X-API-Key", "456")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Create key failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create key status: %d", resp.StatusCode)
		}
		var data map[string]string
		json.NewDecoder(resp.Body).Decode(&data)
		return data["key"]
	}

	getKeys := func() []string {
		req, _ := http.NewRequest("GET", host+"/membership", nil) // GetAll returns map
		req.Header.Set("X-API-Key", "456")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Get keys failed: %v", err)
		}
		defer resp.Body.Close()

		var data map[string][]KeyResponse
		json.NewDecoder(resp.Body).Decode(&data)

		var keys []string
		if kList, ok := data["TestStreamer"]; ok {
			for _, k := range kList {
				keys = append(keys, k.Key)
			}
		}
		return keys
	}

	// 1. Create Key 1
	key1 := createKey()
	time.Sleep(100 * time.Millisecond) // Ensure timestamp difference

	// 2. Create Key 2
	key2 := createKey()
	time.Sleep(100 * time.Millisecond)

	// Verify we have 2 keys
	keys := getKeys()
	if len(keys) != 2 {
		t.Fatalf("Expected 2 keys, got %d", len(keys))
	}

	// 3. Create Key 3 (Should rotate out Key 1)
	key3 := createKey()

	// Verify we still have 2 keys
	keys = getKeys()
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys after rotation, got %d", len(keys))
	}

	// Verify Key 1 is gone
	for _, k := range keys {
		if k == key1 {
			t.Errorf("Key 1 should have been deleted, but it is still present")
		}
	}

	// Verify Key 2 and Key 3 are present
	hasKey2 := false
	hasKey3 := false
	for _, k := range keys {
		if k == key2 {
			hasKey2 = true
		}
		if k == key3 {
			hasKey3 = true
		}
	}

	if !hasKey2 {
		t.Error("Key 2 should be present")
	}
	if !hasKey3 {
		t.Error("Key 3 should be present")
	}
}

func TestMembershipKeyExpiration(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	// Config has KeyTTLDays: 30

	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()
	host := ts.URL

	// 1. Manually insert an expired key
	expiredKey := "expired-key-123"
	// 31 days ago > 30 days TTL
	expiredCreatedAt := time.Now().Add(-31 * 24 * time.Hour).Format(time.RFC3339)

	_, err := app.db.Exec("INSERT INTO membership_keys (key, channel, created_at) VALUES (?, ?, ?)",
		expiredKey, "TestStreamer", expiredCreatedAt)
	if err != nil {
		t.Fatalf("Failed to insert expired key: %v", err)
	}

	// 2. Initial check - key should exist in DB
	var count int
	err = app.db.QueryRow("SELECT COUNT(*) FROM membership_keys WHERE key = ?", expiredKey).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("Key should have been inserted. count=%d, err=%v", count, err)
	}

	// 3. Try to use it (Verify Endpoint)
	req, _ := http.NewRequest("GET", host+"/membership/verify", nil)
	req.Header.Set("X-Membership-Key", expiredKey)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// 4. Expect 401 Unauthorized
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 for expired key, got %d", resp.StatusCode)
	}

	// 5. Verify it was deleted from DB
	err = app.db.QueryRow("SELECT COUNT(*) FROM membership_keys WHERE key = ?", expiredKey).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query key count: %v", err)
	}
	if count != 0 {
		t.Errorf("Expired key should have been deleted, but count is %d", count)
	}

	// 6. Manually insert a VALID key (Just before expiration)
	validKey := "valid-key-456"
	// 29 days ago < 30 days TTL
	validCreatedAt := time.Now().Add(-29 * 24 * time.Hour).Format(time.RFC3339)

	_, err = app.db.Exec("INSERT INTO membership_keys (key, channel, created_at) VALUES (?, ?, ?)",
		validKey, "TestStreamer", validCreatedAt)
	if err != nil {
		t.Fatalf("Failed to insert valid key: %v", err)
	}

	// 7. Try to use it (Verify Endpoint)
	req, _ = http.NewRequest("GET", host+"/membership/verify", nil)
	req.Header.Set("X-Membership-Key", validKey)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// 8. Expect 200 OK
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for valid key, got %d", resp.StatusCode)
	}

	// 9. Verify it still exists in DB
	err = app.db.QueryRow("SELECT COUNT(*) FROM membership_keys WHERE key = ?", validKey).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query key count: %v", err)
	}
	if count != 1 {
		t.Errorf("Valid key should NOT have been deleted, but count is %d", count)
	}
}

func TestMembershipBulkKeyRotation(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()

	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()
	host := ts.URL

	// 1. Manually insert 5 keys with sequential timestamps
	for i := 1; i <= 5; i++ {
		key := fmt.Sprintf("old-key-%d", i)
		// Set timestamps in the past to ensure order, or use simple sequential ones
		createdAt := time.Now().Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		_, err := app.db.Exec("INSERT INTO membership_keys (key, channel, created_at) VALUES (?, ?, ?)",
			key, "TestStreamer", createdAt)
		if err != nil {
			t.Fatalf("Failed to insert initial key %d: %v", i, err)
		}
	}

	// 2. Create a new key via API
	// This should trigger the "DELETE WHERE key IN (...)" for keys 1-4
	createReq, _ := http.NewRequest("POST", host+"/membership/TestStreamer", nil)
	createReq.Header.Set("X-API-Key", "456")
	resp, err := client.Do(createReq)
	if err != nil {
		t.Fatalf("Create key failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create key status: %d", resp.StatusCode)
	}

	// 3. Verify exactly 2 keys remain for this channel
	var count int
	err = app.db.QueryRow("SELECT COUNT(*) FROM membership_keys WHERE channel = ?", "TestStreamer").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query key count: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 keys total, got %d", count)
	}

	// 4. Verify that "old-key-5" (the latest of the previous ones) is still there
	err = app.db.QueryRow("SELECT COUNT(*) FROM membership_keys WHERE key = ?", "old-key-5").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query old-key-5: %v", err)
	}
	if count != 1 {
		t.Error("old-key-5 (the most recent of the initial 5) should still exist")
	}

	// 5. Verify that "old-key-1" is gone
	err = app.db.QueryRow("SELECT COUNT(*) FROM membership_keys WHERE key = ?", "old-key-1").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query old-key-1: %v", err)
	}
	if count != 0 {
		t.Error("old-key-1 should have been deleted")
	}
}

func TestEnsureMembershipKeys(t *testing.T) {
	app := setupTestApp(t)
	defer app.db.Close()
	ctx := context.Background()

	// 1. Configure a membership channel
	app.config.Membership = []string{"InitialChannel"}

	// 2. Run EnsureMembershipKeys
	err := app.EnsureMembershipKeys(ctx)
	if err != nil {
		t.Fatalf("EnsureMembershipKeys failed: %v", err)
	}

	// 3. Verify key was created
	keys, err := app.GetMembershipKeys(ctx, "InitialChannel")
	if err != nil {
		t.Fatalf("GetMembershipKeys failed: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("Expected 1 key for InitialChannel, got %d", len(keys))
	}

	// 4. Run it again - should NOT create another key
	err = app.EnsureMembershipKeys(ctx)
	if err != nil {
		t.Fatalf("Second EnsureMembershipKeys failed: %v", err)
	}

	keys2, err := app.GetMembershipKeys(ctx, "InitialChannel")
	if err != nil {
		t.Fatalf("Second GetMembershipKeys failed: %v", err)
	}
	if len(keys2) != 1 {
		t.Errorf("Expected still 1 key for InitialChannel, got %d", len(keys2))
	}
}

func TestStreamMetadataVisibility(t *testing.T) {
	app := setupTestApp(t)
	seedTestData(t, app)
	defer app.db.Close()

	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()
	host := ts.URL

	// Helper to check status
	checkStatus := func(url string, expectedStatus int) {
		req, _ := http.NewRequest("GET", url, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != expectedStatus {
			t.Errorf("GET %s: expected status %d, got %d", url, expectedStatus, resp.StatusCode)
		}
	}

	// 1. Verify /stream/{id} is public for Members stream (No Auth)
	// Members Stream ID: eeb65mIOpfs (from seedTestData)
	t.Log("[1] Checking /stream/{id} for Members stream (No Auth)")
	checkStatus(host+"/stream/eeb65mIOpfs", http.StatusNotFound)

	// 2. Verify /stream/{id} is public for Public stream (No Auth)
	// Public Stream ID: 5jvNKUzrI4Q
	t.Log("[2] Checking /stream/{id} for Public stream (No Auth)")
	checkStatus(host+"/stream/5jvNKUzrI4Q", http.StatusOK)

	// 3. Verify /info returns ALL streams (No Auth)
	t.Log("[3] Checking /info returns all streams (No Auth)")
	req, _ := http.NewRequest("GET", host+"/info", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Info request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Wrong status: Got %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var streams []StreamMetadataOutput
	if err := json.NewDecoder(resp.Body).Decode(&streams); err != nil {
		t.Fatalf("Failed to decode /info response: %v", err)
	}

	// We seeded 4 transcripts in seedTestData
	if len(streams) != 4 {
		t.Errorf("Expected 4 streams in /info, got %d", len(streams))
	}

	// Verify "Members" stream is present
	foundMembers := false
	for _, s := range streams {
		if s.ID == "eeb65mIOpfs" && s.StreamType == "Members" {
			foundMembers = true
			break
		}
	}
	if !foundMembers {
		t.Error("Members stream 'eeb65mIOpfs' should be present in /info")
	}
}

func TestKeyCreationOnNonConfiguredChannel(t *testing.T) {
	app := setupTestApp(t)
	seedTestData(t, app)
	defer app.db.Close()

	mux := http.NewServeMux()
	app.InitServerEndpoints(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	client := ts.Client()
	host := ts.URL

	createReq, _ := http.NewRequest("POST", host+"/membership/NonChannel", nil)
	createReq.Header.Set("X-API-Key", "456")
	createResp, err := client.Do(createReq)
	if err != nil {
		t.Fatalf("Create key failed: %v", err)
	}
	if createResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Got %d, want %d", createResp.StatusCode, http.StatusBadRequest)
	}
}
