package ImageManager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type IMAGESCOPE int

const DOCKER_AUTH = "auth.docker.io"
const DOCKER_REGISTRY = "registry.docker.io"

const (
	PULL IMAGESCOPE = iota
	PUSH
	PULLPUSH
)

func (i IMAGESCOPE) String() string {
	return [...]string{"pull", "push", "pull,push"}[i]
}

type AuthenticationResponse struct {
	Token        string `json:"token"`
	Access_token string `json:"access_token"`
	Expires_in   int    `json:"expires_in"`
	Issued_at    string `json:"issued_at"`
}

type Authenticator interface {
	Authenticate(ctx context.Context, req *http.Request, image string, scope IMAGESCOPE) error
}

type imageRequest struct {
	image string
	scope IMAGESCOPE
}

type OCIAuthenticator struct {
	cache map[imageRequest]AuthenticationResponse
}

func authenticate(ctx context.Context, image string, scope IMAGESCOPE) (*AuthenticationResponse, error) {
	url := fmt.Sprintf("https://%s/token?service=%s&scope=repository:library/%s:%s", DOCKER_AUTH, DOCKER_REGISTRY, image, scope.String())
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	client := http.DefaultClient
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	var authResponse AuthenticationResponse
	err = json.NewDecoder(res.Body).Decode(&authResponse)
	if err != nil {
		return nil, err
	}
	return &authResponse, nil
}

func (o *OCIAuthenticator) Authenticate(context context.Context, request *http.Request, image string, scope IMAGESCOPE) error {
	if o.cache == nil {
		o.cache = make(map[imageRequest]AuthenticationResponse)
	}
	cache, ok := o.cache[imageRequest{image, scope}]
	if ok {
		issued_at, err := time.Parse(time.RFC3339Nano, cache.Issued_at)
		if err != nil {
			return err
		}
		if time.Now().After(issued_at.Add(time.Duration(cache.Expires_in) * time.Second)) {
			delete(o.cache, imageRequest{image, scope})
		} else {
			request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", cache.Token))
			return nil
		}
	}
	authResponse, err := authenticate(context, image, scope)
	if err != nil {
		return err
	}
	o.cache[imageRequest{image, scope}] = *authResponse
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", authResponse.Token))
	return nil
}
