package adf

import (
	"encoding/json"
	"fmt"
	"github.com/prebid/openrtb/v20/openrtb2"
	"github.com/prebid/prebid-server/v2/adapters"
	"github.com/prebid/prebid-server/v2/config"
	"github.com/prebid/prebid-server/v2/errortypes"
	"github.com/prebid/prebid-server/v2/openrtb_ext"
	"net/http"
	"slices"
)

type adapter struct {
	endpoint         string
	extraAdapterInfo map[string][]string
}

type adfRequestExt struct {
	openrtb_ext.ExtRequest
	PriceType string `json:"pt"`
}

// Builder builds a new instance of the Adf adapter for the given bidder with the given config.
func Builder(bidderName openrtb_ext.BidderName, config config.Adapter, server config.Server) (adapters.Bidder, error) {

	var unmarshalledExtraAdapterInfo map[string][]string
	if err := json.Unmarshal([]byte(config.ExtraAdapterInfo), &unmarshalledExtraAdapterInfo); err != nil {
		unmarshalledExtraAdapterInfo = make(map[string][]string)
	}

	bidder := &adapter{
		endpoint:         config.Endpoint,
		extraAdapterInfo: unmarshalledExtraAdapterInfo,
	}
	return bidder, nil
}

func (a *adapter) MakeRequests(request *openrtb2.BidRequest, requestInfo *adapters.ExtraRequestInfo) ([]*adapters.RequestData, []error) {
	var errors []error
	var validImps = make([]openrtb2.Imp, 0, len(request.Imp))
	priceType := ""
	suppressPbadslotDetails := getSuppressPbadslot(a.extraAdapterInfo)

	for _, imp := range request.Imp {
		imp.Ext = pbadslotEditor(imp.Ext, suppressPbadslotDetails, &errors)

		var bidderExt adapters.ExtImpBidder
		if err := json.Unmarshal(imp.Ext, &bidderExt); err != nil {
			errors = append(errors, &errortypes.BadInput{
				Message: err.Error(),
			})
			continue
		}

		var adfImpExt openrtb_ext.ExtImpAdf
		if err := json.Unmarshal(bidderExt.Bidder, &adfImpExt); err != nil {
			errors = append(errors, &errortypes.BadInput{
				Message: err.Error(),
			})
			continue
		}

		imp.TagID = adfImpExt.MasterTagID.String()
		validImps = append(validImps, imp)

		// If imps specify priceType they should all be the same. If they differ, only the first one will be used
		if adfImpExt.PriceType != "" && priceType == "" {
			priceType = adfImpExt.PriceType
		}
	}

	if priceType != "" {
		requestExt := adfRequestExt{}
		var err error

		if len(request.Ext) > 0 {
			if err = json.Unmarshal(request.Ext, &requestExt); err != nil {
				errors = append(errors, err)
			}
		}

		if err == nil {
			requestExt.PriceType = priceType

			if request.Ext, err = json.Marshal(&requestExt); err != nil {
				errors = append(errors, err)
			}
		}
	}

	request.Imp = validImps

	requestJSON, err := json.Marshal(request)
	if err != nil {
		errors = append(errors, err)
		return nil, errors
	}

	requestData := &adapters.RequestData{
		Method: "POST",
		Uri:    a.endpoint,
		Body:   requestJSON,
	}

	return []*adapters.RequestData{requestData}, errors
}

func (a *adapter) MakeBids(request *openrtb2.BidRequest, requestData *adapters.RequestData, responseData *adapters.ResponseData) (*adapters.BidderResponse, []error) {
	if responseData.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	if responseData.StatusCode == http.StatusBadRequest {
		err := &errortypes.BadInput{
			Message: "Unexpected status code: 400. Bad request from publisher.",
		}
		return nil, []error{err}
	}

	if responseData.StatusCode != http.StatusOK {
		err := &errortypes.BadServerResponse{
			Message: fmt.Sprintf("Unexpected status code: %d.", responseData.StatusCode),
		}
		return nil, []error{err}
	}

	var response openrtb2.BidResponse
	if err := json.Unmarshal(responseData.Body, &response); err != nil {
		return nil, []error{err}
	}

	bidResponse := adapters.NewBidderResponseWithBidsCapacity(len(request.Imp))
	bidResponse.Currency = response.Cur
	var errors []error
	for _, seatBid := range response.SeatBid {
		for i, bid := range seatBid.Bid {
			bidType, err := getMediaTypeForBid(bid)
			if err != nil {
				errors = append(errors, err)
				continue
			}
			bidResponse.Bids = append(bidResponse.Bids, &adapters.TypedBid{
				Bid:     &seatBid.Bid[i],
				BidType: bidType,
			})
		}
	}

	return bidResponse, errors
}

func getMediaTypeForBid(bid openrtb2.Bid) (openrtb_ext.BidType, error) {
	if bid.Ext != nil {
		var bidExt openrtb_ext.ExtBid
		err := json.Unmarshal(bid.Ext, &bidExt)
		if err == nil && bidExt.Prebid != nil {
			return openrtb_ext.ParseBidType(string(bidExt.Prebid.Type))
		}
	}

	return "", &errortypes.BadServerResponse{
		Message: fmt.Sprintf("Failed to parse impression \"%s\" mediatype", bid.ImpID),
	}
}

func getSuppressPbadslot(extraAdapterInfo map[string][]string) bool {
	if value, keyExists := extraAdapterInfo["suppresskeywords"]; keyExists {
		if slices.Contains(value, "pbadslot") {
			return true
		}
	}
	return false
}

func pbadslotEditor(extContent json.RawMessage, suppressPbadslotdetails bool, errors *[]error) json.RawMessage {
	if !suppressPbadslotdetails {
		return extContent
	}

	var marshalledEditedExtContent []byte

	var unmarshalledExtContent map[string]json.RawMessage
	var unmarshalledData map[string]json.RawMessage
	replacementPerformed := false

	if err := json.Unmarshal(extContent, &unmarshalledExtContent); err != nil {
		*errors = append(*errors, &errortypes.BadInput{
			Message: err.Error(),
		})
		return extContent
	}

	if dataVal, ok := unmarshalledExtContent["data"]; ok {
		if err := json.Unmarshal(dataVal, &unmarshalledData); err != nil {
			*errors = append(*errors, &errortypes.BadInput{
				Message: err.Error(),
			})
			return extContent
		}

		if _, ok := unmarshalledData["pbadslot"]; ok {
			delete(unmarshalledData, "pbadslot")
			marshalledEditedData, _ := json.Marshal(unmarshalledData)
			unmarshalledExtContent["data"] = marshalledEditedData
			marshalledEditedExtContent, _ = json.Marshal(unmarshalledExtContent)
			replacementPerformed = true
		}
	}

	if replacementPerformed {
		return marshalledEditedExtContent
	} else {
		return extContent
	}
}
