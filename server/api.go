package main

import (
	"fmt"
	"net/http"
	"reflect"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost-plugin-api/experimental/common"
	"github.com/mattermost/mattermost-server/v6/model"
)

func (p *Plugin) initializeAPI() {
	p.router = mux.NewRouter()

	p.router.HandleFunc("/authorize", p.middleware(p.handleAuthorize)).Methods(http.MethodPost)
	p.router.HandleFunc("/deny", p.middleware(p.handleDeny)).Methods(http.MethodPost)
	p.router.HandleFunc("/cancel", p.middleware(p.handleCancel)).Methods(http.MethodPost)
}

func (p *Plugin) middleware(handler func(http.ResponseWriter, Authorization, string)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-ID")
		if userID == "" {
			common.SlackAttachmentError(w, "Error: Not authorized")
			return
		}

		request := model.PostActionIntegrationRequestFromJson(r.Body)
		if request == nil {
			common.SlackAttachmentError(w, "Error: invalid request")
			return
		}

		idInt, ok := request.Context[IDContextField]
		if !ok {
			common.SlackAttachmentError(w, "Cannot find the ID of the authorization")
			return
		}

		id, ok := idInt.(string)
		if !ok {
			common.SlackAttachmentError(w, "Cannot parse the ID of the authorization")
			return
		}

		a := Authorization{}
		p.mm.KV.Get(id, &a)

		handler(w, a, userID)
	}
}
func (p *Plugin) handleAuthorize(w http.ResponseWriter, a Authorization, userID string) {
	if a.hasAuthorized(userID) {
		common.SlackAttachmentError(w, "You already authorized this.")
		return
	}

	found := false
	for k, _ := range a.AuthorizerPosts {
		if userID == k {
			found = true
		}
	}

	if !found {
		common.SlackAttachmentError(w, "You cannot authorize this action.")
		return
	}

	a.Authorizations = append(a.Authorizations, userID)
	if len(a.Authorizations) < a.AuthorizationsNeeded {
		err := p.updatePosts(a)
		if err != nil {
			common.SlackAttachmentError(w, err.Error())
			return
		}

		_, err = p.mm.KV.Set(a.ID, a)
		if err != nil {
			common.SlackAttachmentError(w, err.Error())
		}

		_, _ = w.Write((&model.PostActionIntegrationResponse{}).ToJson())
		return
	}

	conf := p.mm.Configuration.GetConfig()
	reflected := reflect.ValueOf(conf).Elem()
	for _, f := range a.Field[1:] {
		reflected = reflected.FieldByName(f)
	}

	valueType := reflected.Type()
	if valueType.Kind() == reflect.Ptr {
		reflected = reflected.Elem()
		valueType = reflected.Type()
	}
	switch reflected.Type().Kind() {
	case reflect.String:
		reflected.SetString(a.Value)
	case reflect.Bool:
		reflected.SetBool(a.Value == "true")
	case reflect.Int:
		v, _ := strconv.ParseInt(a.Value, 10, 32)
		reflected.SetInt(v)
	case reflect.Int64:
		p.mm.Log.Debug("good path")
		v, _ := strconv.ParseInt(a.Value, 10, 64)
		reflected.SetInt(v)
	}

	err := p.mm.Configuration.SaveConfig(conf)
	if err != nil {
		p.mm.Log.Debug("error applying configuration change", "error", err)
	}
	p.finishMPA(a)
}

func (p *Plugin) handleDeny(w http.ResponseWriter, a Authorization, userID string) {
	// TODO
	_, _ = w.Write((&model.PostActionIntegrationResponse{}).ToJson())
}

func (p *Plugin) handleCancel(w http.ResponseWriter, a Authorization, userID string) {
	// TODO
	_, _ = w.Write((&model.PostActionIntegrationResponse{}).ToJson())
}

func interactiveDialogError(w http.ResponseWriter, message string) {
	resp := model.SubmitDialogResponse{
		Error: message,
	}

	_, _ = w.Write(resp.ToJson())
}

func (p *Plugin) getAPIURL() string {
	baseURL := p.mm.Configuration.GetConfig().ServiceSettings.SiteURL
	return fmt.Sprintf("%s/plugins/%s/", *baseURL, "com.mattermost.mpa")
}
