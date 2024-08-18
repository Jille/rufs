package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/Jille/convreq"
	"github.com/Jille/convreq/respond"
	"github.com/Jille/rufs/security"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ca *security.CAKeyPair

	oidcProvider *oidc.Provider
	oauthConfig  oauth2.Config
)

type Config struct {
	Certdir      string `json:"certdir"`
	OIDCProvider string `json:"oidc_provider"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	ReturnURL    string `json:"return_url"`
	Port         int    `json:"port"`
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <configfile>", os.Args[0])
	}
	c, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to read config file %q: %v", os.Args[1], err)
	}
	var cfg Config
	if err := json.Unmarshal(c, &cfg); err != nil {
		log.Fatalf("Failed to read config file %q: %v", os.Args[1], err)
	}

	if cfg.Certdir == "" {
		log.Fatalf("Configuration setting \"certdir\" is empty")
	}
	if cfg.OIDCProvider == "" {
		log.Fatalf("Configuration setting \"oidc_provider\" is empty")
	}
	if cfg.ClientID == "" {
		log.Fatalf("Configuration setting \"client_id\" is empty")
	}
	if cfg.ClientSecret == "" {
		log.Fatalf("Configuration setting \"client_secret\" is empty")
	}
	if cfg.ReturnURL == "" {
		log.Fatalf("Configuration setting \"return_url\" is empty")
	}
	if cfg.Port == 0 {
		log.Fatalf("Configuration setting \"port\" is not set")
	}

	ca, err = security.LoadCAKeyPair(cfg.Certdir)
	if err != nil {
		log.Fatalf("Failed to load CA key pair: %v", err)
	}

	oidcProvider, err = oidc.NewProvider(context.Background(), cfg.OIDCProvider)
	if err != nil {
		log.Fatalf("Failed to discover openid connect provider: %v", err)
	}
	oauthConfig = oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     oidcProvider.Endpoint(),
		RedirectURL:  cfg.ReturnURL,
		Scopes:       []string{"openid"},
	}

	http.Handle("/oauth2_return", convreq.Wrap(oauth2Return))
	http.Handle("/request_token", convreq.Wrap(requestToken))
	http.Handle("/", convreq.Wrap(mainPage))
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), nil))
}

type requestTokenGet struct {
	Hostname  string `schema:"hostname"`
	ReturnURL string `schema:"return_url"`
}

func requestToken(get requestTokenGet) convreq.HttpResponse {
	s := base64.URLEncoding.EncodeToString([]byte(get.Hostname)) + ":" + base64.URLEncoding.EncodeToString([]byte(get.ReturnURL))
	u := oauthConfig.AuthCodeURL(s)
	return respond.Redirect(302, u)
}

type oauth2ReturnGet struct {
	Code  string `schema:"code"`
	State string `schema:"state"`
}

func oauth2Return(ctx context.Context, get oauth2ReturnGet) convreq.HttpResponse {
	t, err := oauthConfig.Exchange(ctx, get.Code)
	if err != nil {
		return respond.BadRequest("Oauth failed: " + err.Error())
	}
	userInfo, err := oidcProvider.UserInfo(ctx, oauth2.StaticTokenSource(t))
	if err != nil {
		return respond.Error(err)
	}
	var ui UserInfo
	if err := userInfo.Claims(&ui); err != nil {
		return respond.Error(err)
	}

	sp := strings.Split(get.State, ":")
	hostname, err := base64.URLEncoding.DecodeString(sp[0])
	if err != nil {
		return respond.BadRequest("bad state")
	}
	returnURL, err := base64.URLEncoding.DecodeString(sp[1])
	if err != nil {
		return respond.BadRequest("bad state")
	}

	username := strings.Split(ui.PreferredUsername, "@")[0]
	username = strings.ReplaceAll(username, "-", "")
	username += "-" + string(hostname)

	token := ca.CreateToken(username)

	u, err := url.Parse(string(returnURL))
	if err != nil {
		return respond.BadRequest("Invalid return_url: " + err.Error())
	}
	q := u.Query()
	q.Set("username", username)
	q.Set("token", token)
	q.Set("circle", ca.Name())
	q.Set("fingerprint", ca.Fingerprint())

	if len(returnURL) == 0 {
		var ret string
		for k, vs := range q {
			ret += k + ":" + vs[0] + "\n"
		}
		return respond.String(ret)
	}

	u.RawQuery = q.Encode()
	return respond.Redirect(302, u.String())
}

type UserInfo struct {
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
}

type mainPageGet struct {
	Hostname  string `schema:"hostname"`
	ReturnURL string `schema:"return_url"`
}

var mainPageTpl = template.Must(template.New("").Parse(`
		<html>
			<head>
			</head>
			<body>
				<form method="GET" action="/request_token">
					Hostname: <input name="hostname" value="{{.Hostname}}" />
					<input type="hidden" name="return_url" value="{{.ReturnURL}}" />
					<input type="submit" value="Continue" />
				</form>
			</body>
		</html>
	`))

func mainPage(get mainPageGet) convreq.HttpResponse {
	return respond.RenderTemplate(mainPageTpl, get)
}
