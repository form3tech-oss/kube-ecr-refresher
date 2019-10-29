// Copyright 2019 Form3 Financial Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package refresher

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/aws/aws-sdk-go/service/ecr/ecriface"

	log "github.com/sirupsen/logrus"
)

// amazonECRAuthenticationData is a wrapper for authentication data for an Amazon ECR registry.
type AmazonECRAuthenticationData struct {
	expiration time.Time
	Password   string
	Server     string
	Username   string
}

// AmazonECRAuthenticationDataRefresher knows how to refresh authentication data for an Amazon ECR registry.
type AmazonECRAuthenticationDataRefresher struct {
	current   *AmazonECRAuthenticationData
	ecrClient ecriface.ECRAPI
}

// New returns a new instance of AmazonECRAuthenticationDataRefresher.
func New() (*AmazonECRAuthenticationDataRefresher, error) {
	s, err := session.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize AWS session: %v", err)
	}
	return &AmazonECRAuthenticationDataRefresher{ecrClient: ecr.New(s)}, nil
}

// Get returns the current Amazon ECR authentication data.
func (r *AmazonECRAuthenticationDataRefresher) Get() (*AmazonECRAuthenticationData, error) {
	if r.current != nil {
		return r.current, nil
	}
	return nil, fmt.Errorf("no Amazon ECR authentication data currently exists")
}

// Run runs the refresh process.
func (r *AmazonECRAuthenticationDataRefresher) Run() {
	for {
		log.Debugf("Attempting to refresh Amazon ECR authentication data")
		d, err := r.refresh()
		if err != nil {
			log.Errorf("Failed to refresh Amazon ECR authentication data: %v", err)
			r.current = nil
			u := time.Now().Add(1 * time.Minute)
			log.Debugf("Holding on refreshing Amazon ECR authentication data until %s", u.Format(time.RFC3339))
			time.Sleep(time.Until(u))
		} else {
			log.Debug("Amazon ECR authentication data refreshed")
			r.current = d
			u := d.expiration.Add(-1 * time.Minute)
			log.Debugf("Holding on refreshing Amazon ECR authentication data until %s", u.Format(time.RFC3339))
			time.Sleep(time.Until(u))
		}
	}
}

// refresh attempts to return fresh Amazon ECR authentication data.
func (r *AmazonECRAuthenticationDataRefresher) refresh() (*AmazonECRAuthenticationData, error) {
	o, err := r.ecrClient.GetAuthorizationToken(&ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return nil, err
	}
	if len(o.AuthorizationData) != 1 {
		return nil, fmt.Errorf("expected a single result (got %d)", len(o.AuthorizationData))
	}
	e := *o.AuthorizationData[0].ExpiresAt
	s := strings.TrimPrefix(*o.AuthorizationData[0].ProxyEndpoint, "https://")
	v, err := base64.StdEncoding.DecodeString(*o.AuthorizationData[0].AuthorizationToken)
	if err != nil {
		return nil, err
	}
	t := strings.Split(string(v), ":")
	if len(t) != 2 {
		return nil, fmt.Errorf("AWS returned a malformed token")
	}
	return &AmazonECRAuthenticationData{
		expiration: e,
		Server:     s,
		Password:   t[1],
		Username:   t[0],
	}, nil
}
