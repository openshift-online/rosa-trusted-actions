package ocm

import (
	"context"
	"fmt"

	azv1 "github.com/openshift-online/ocm-sdk-go/authorizations/v1"
)

type Authorization interface {
	AccessReview(ctx context.Context, username, action, resourceType string) (allowed bool, err error)
}

type authorization service

var _ Authorization = &authorization{}

func (a authorization) AccessReview(ctx context.Context, username, action, resourceType string) (allowed bool, err error) {
	con := a.client.connection
	accessReview := con.Authorizations().V1().AccessReview()

	request, err := azv1.NewAccessReviewRequest().
		AccountUsername(username).
		Action(action).
		ResourceType(resourceType).
		Build()
	if err != nil {
		return false, err
	}

	postResp, err := accessReview.Post().
		Request(request).
		SendContext(ctx)
	if err != nil {
		return false, err
	}

	response, ok := postResp.GetResponse()
	if !ok {
		return false, fmt.Errorf("empty response from authorization post request")
	}

	return response.Allowed(), nil
}
