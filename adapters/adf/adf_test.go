package adf

import (
	"bytes"
	"encoding/json"
	"github.com/prebid/openrtb/v20/openrtb2"
	"testing"

	"github.com/prebid/prebid-server/v2/adapters/adapterstest"
	"github.com/prebid/prebid-server/v2/config"
	"github.com/prebid/prebid-server/v2/openrtb_ext"
)

func TestJsonSamples(t *testing.T) {
	bidder, buildErr := Builder(openrtb_ext.BidderAdf, config.Adapter{
		Endpoint:         "https://adx.adform.net/adx/openrtb",
		ExtraAdapterInfo: "{\"suppressPbadslotdetails\": \"true\"}",
	}, config.Server{ExternalUrl: "http://hosturl.com", GvlID: 1, DataCenter: "2"})

	if buildErr != nil {
		t.Fatalf("Builder returned unexpected error %v", buildErr)
	}

	adapterstest.RunJSONBidderTest(t, "adftest", bidder)
}

func TestMakeRequestsWithPbadslotReplacement(t *testing.T) {
	bidder, buildErr := Builder(openrtb_ext.BidderAdf, config.Adapter{ExtraAdapterInfo: "{\"suppresskeywords\": [\"pbadslot\", \"otherkeyword\"]}"}, config.Server{})

	if buildErr != nil {
		t.Fatalf("Builder returned unexpected error %v", buildErr)
	}

	mockedReq := &openrtb2.BidRequest{
		Imp: []openrtb2.Imp{{
			ID:  "1_1",
			Ext: json.RawMessage(`{"bidder":{},"data":{"adserver":{"name":"test","adslot":"adslot123"},"pbadslot":"pbadslot123"}}`),
		}},
	}
	expected_imp := json.RawMessage(`{"bidder":{},"data":{"adserver":{"name":"test","adslot":"adslot123"}}}`)

	reqs, _ := bidder.MakeRequests(mockedReq, nil)

	if len(reqs) != 1 {
		t.Fatalf("MakeRequests returned %d requests, expected 1", len(reqs))
	}

	var unmarshalledReqsContent map[string]json.RawMessage
	if err := json.Unmarshal(reqs[0].Body, &unmarshalledReqsContent); err != nil {
		t.Fatalf("Unexpected request body: %v", string(reqs[0].Body))
	}

	var unmarshalledImpsContent []map[string]json.RawMessage
	if err := json.Unmarshal(unmarshalledReqsContent["imp"], &unmarshalledImpsContent); err != nil {
		t.Fatalf("Unexpected imp content: %v", string(unmarshalledReqsContent["imp"]))
	}

	if len(unmarshalledImpsContent) != 1 {
		t.Fatalf("Unexpected number of imps: %d", len(unmarshalledImpsContent))
	}

	if !bytes.Equal(unmarshalledImpsContent[0]["ext"], expected_imp) {
		t.Fatalf("Unexpected imp: %v", string(unmarshalledImpsContent[0]["ext"]))
	}
}

func TestPbadslotEditorReturnsOriginalInputIfOriginalInputIsMalformed(t *testing.T) {
	var errors []error
	brokenExtContent := json.RawMessage(`{1:2}`)

	newBrokenExtContent := pbadslotEditor(brokenExtContent, true, &errors)

	if !bytes.Equal(brokenExtContent, newBrokenExtContent) {
		t.Fatalf("Unexpected modification of brokenExtContent: %v", string(newBrokenExtContent))
	}
	if len(errors) != 1 {
		t.Fatalf("Error is not recorded")
	}

	brokenExtContent = json.RawMessage(`{"data":{"adserver":{"name":"test","adslot":"adslot123"},1:2}}`)

	newBrokenExtContent = pbadslotEditor(brokenExtContent, true, &errors)

	if !bytes.Equal(brokenExtContent, newBrokenExtContent) {
		t.Fatalf("Unexpected modification of brokenExtContent: %v", string(newBrokenExtContent))
	}
	if len(errors) != 2 {
		t.Fatalf("Unexpcted error handling: %v", errors)
	}
}

func TestExtProcessing(t *testing.T) {
	var errors []error
	extRequiringEditing := json.RawMessage(`{"data":{"adserver":{"name":"test","adslot":"adslot123"},"pbadslot":"pbadslot123"}}`)
	expectedEditedContent := json.RawMessage(`{"data":{"adserver":{"name":"test","adslot":"adslot123"}}}`)

	newExt := pbadslotEditor(extRequiringEditing, true, &errors)

	if !bytes.Equal(newExt, expectedEditedContent) {
		t.Fatalf("Modification of extRequiringEditing failed: %v", string(newExt))
	}

	newExt = pbadslotEditor(extRequiringEditing, false, &errors)

	if !bytes.Equal(newExt, extRequiringEditing) {
		t.Fatalf("Unexpected modification of extRequiringEditing: %v", string(newExt))
	}

	extNotRequiringEditing := json.RawMessage(`{"data":{"adserver":{"name":"test","adslot":"adslot123"},"notpbadslot":"notpbadslot"}}`)

	newExt = pbadslotEditor(extNotRequiringEditing, true, &errors)

	if !bytes.Equal(newExt, extNotRequiringEditing) {
		t.Fatalf("Unexpected modification of extNotRequiringEditing: %v", string(newExt))
	}
}

func TestJsonKeyValueRemoval(t *testing.T) {
	jsonExtContent := json.RawMessage(`{"key1":"value1","key2":"value2"}`)
	expectedJsonExtContent := json.RawMessage(`{"key1":"value1"}`)

	var unmarshalledExtContent map[string]string
	if err := json.Unmarshal(jsonExtContent, &unmarshalledExtContent); err != nil {
		t.Fatalf("Failed to unmarshal jsonExtContent: %v", err)
	}
	delete(unmarshalledExtContent, "key2")
	jsonModifiedExtContent, err := json.Marshal(unmarshalledExtContent)

	if err != nil {
		t.Fatalf("Failed to marshal unmarshalledExtContent: %v", err)
	}

	if !bytes.Equal(jsonModifiedExtContent, expectedJsonExtContent) {
		t.Fatalf("jsonModifiedExtContent does not match expectedJsonExtContent: %v", string(jsonModifiedExtContent))
	}
}

func TestUnmarshalAndMarshalRemoveRedundantSpaces(t *testing.T) {
	//spaces are crucial here to show Unmarshal + Marshal removes spaces
	jsonContent := json.RawMessage(`{"key1":         "value1",       "key2":                   "value2"}`)
	expectedJsonContent := json.RawMessage(`{"key1":"value1","key2":"value2"}`)

	var unmarshalledJsonContent map[string]string
	if err := json.Unmarshal(jsonContent, &unmarshalledJsonContent); err != nil {
		t.Fatalf("Failed to unmarshal jsonContent: %v", err)
	}
	marshalledJsonContent, err := json.Marshal(unmarshalledJsonContent)

	if err != nil {
		t.Fatalf("Failed to marshal unmarshalledJsonContent: %v", err)
	}
	if !bytes.Equal(marshalledJsonContent, expectedJsonContent) {
		t.Fatalf("MarshalledJsonContent does not match expectedJsonContent: %v", string(marshalledJsonContent))
	}
}

func TestGetSuppressPbadslot(t *testing.T) {
	extraAdapterInfo := map[string][]string{"key1": {"value1", "value2"}, "key2": {"newvalue"}}

	shouldSuppressPbadslot := getSuppressPbadslot(extraAdapterInfo)

	if shouldSuppressPbadslot {
		t.Fatalf("shouldSuppressPbadslot was meant to be false")
	}

	extraAdapterInfo = map[string][]string{"suppresskeywords": {"value1", "value2"}, "key2": {"newvalue"}}

	shouldSuppressPbadslot = getSuppressPbadslot(extraAdapterInfo)

	if shouldSuppressPbadslot {
		t.Fatalf("shouldSuppressPbadslot was meant to be false")
	}

	extraAdapterInfo = map[string][]string{"suppresskeywords": {"value1", "pbadslot"}, "key2": {"newvalue"}}

	shouldSuppressPbadslot = getSuppressPbadslot(extraAdapterInfo)

	if !shouldSuppressPbadslot {
		t.Fatalf("shouldSuppressPbadslot was meant to be true")
	}
}
