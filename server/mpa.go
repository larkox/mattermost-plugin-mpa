package main

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost-server/v6/model"
)

type Authorization struct {
	ID                   string
	Field                []string
	Value                string
	AuthorizerPosts      map[string]string
	ModifyierPost        string
	ModifyierUserID      string
	Authorizations       []string
	AuthorizationsNeeded int
}

func (a Authorization) getAuthorizerText(username string, authorizerNames []string) string {
	out := fmt.Sprintf("User @%s requested to modify `%s` to the value `%s`.", username, strings.Join(a.Field, "->"), a.Value)
	if len(authorizerNames) > 0 {
		out += fmt.Sprintf("\nAlready authorized by: %s", strings.Join(authorizerNames, ", "))
	}

	return out
}

func (a Authorization) getModifyierText(toAuthorizeNames []string, authorizedNames []string) string {
	out := fmt.Sprintf("You have requested to modify %s to the value %s.\n", strings.Join(a.Field, "->"), a.Value)
	if len(authorizedNames) > 0 {
		out += fmt.Sprintf("This change has already been authorized by: %s.\n", strings.Join(authorizedNames, ", "))
	}
	out += fmt.Sprintf("Waiting for authorizations from %s. You need %d more authorizations.", strings.Join(toAuthorizeNames, ", "), a.AuthorizationsNeeded-len(a.Authorizations))
	return out
}

func (p *Plugin) StartMPA(a Authorization) error {
	a.ID = model.NewId()
	a.AuthorizationsNeeded = p.getNeededAuthorizations()

	u, err := p.mm.User.Get(a.ModifyierUserID)
	if err != nil {
		return err
	}

	if !p.canModify(a.ModifyierUserID) {
		return fmt.Errorf("no permissions to modify the config")
	}

	authorizerText := a.getAuthorizerText(u.Username, nil)
	postIDs, names, err := p.sendAuthorizations(authorizerText, a)
	if err != nil {
		return err
	}

	a.AuthorizerPosts = postIDs

	post := &model.Post{
		Message: a.getModifyierText(names, nil),
	}

	model.ParseSlackAttachment(post, []*model.SlackAttachment{getModifyierAttachment(p.getAPIURL(), a.ID)})
	err = p.mm.Post.DM(p.BotID, a.ModifyierUserID, post)
	if err != nil {
		return err
	}

	a.ModifyierPost = post.Id

	_, err = p.mm.KV.Set(a.ID, a)
	if err != nil {
		return err
	}
	return nil
}

func (p *Plugin) finishMPA(a Authorization) {
	users := []*model.User{}
	authorizerNames := []string{}
	for k, _ := range a.AuthorizerPosts {
		u, err := p.mm.User.Get(k)
		if err != nil {
			continue
		}

		users = append(users, u)
		if a.hasAuthorized(u.Id) {
			authorizerNames = append(authorizerNames, "@"+u.Username)
		}
	}

	_ = p.mm.Post.DM(p.BotID, a.ModifyierUserID, &model.Post{Message: "Your process for MPA has ended. Check the original post."})

	modifyierName := "User with ID " + a.ModifyierUserID
	modifyier, err := p.mm.User.Get(a.ModifyierUserID)
	if err == nil {
		modifyierName = "@" + modifyier.Username
	}

	for _, u := range users {
		post, err := p.mm.Post.GetPost(a.AuthorizerPosts[u.Id])
		if err != nil {
			continue
		}

		post.Message = fmt.Sprintf("%s has been authorized to modify %s to `%s` by %s.", modifyierName, strings.Join(a.Field, "->"), a.Value, strings.Join(authorizerNames, ", "))
		model.ParseSlackAttachment(post, []*model.SlackAttachment{})

		_ = p.mm.Post.UpdatePost(post)
	}

	if modifyier == nil {
		return
	}

	post, err := p.mm.Post.GetPost(a.ModifyierPost)
	if err != nil {
		return
	}

	post.Message = fmt.Sprintf("Your request to modify %s to %s has been authorized by %s and it is already applied.", strings.Join(a.Field, "->"), a.Value, strings.Join(authorizerNames, ", "))
	model.ParseSlackAttachment(post, []*model.SlackAttachment{})

	_ = p.mm.Post.UpdatePost(post)
}

func (p *Plugin) canModify(userID string) bool {
	return true
}

func (p *Plugin) sendAuthorizations(text string, a Authorization) (postsIDs map[string]string, names []string, err error) {
	postsIDs = map[string]string{}
	names = []string{}

	users := []*model.User{}
	for _, v := range p.getAuthorizersIDs(a.ModifyierUserID) {
		u, err := p.mm.User.Get(v)
		if err != nil {
			return nil, nil, err
		}
		users = append(users, u)
	}

	for _, u := range users {
		post := &model.Post{
			Message: text,
		}

		model.ParseSlackAttachment(post, []*model.SlackAttachment{
			getAuthorizerAttachment(p.getAPIURL(), a.ID),
		})

		err = p.mm.Post.DM(p.BotID, u.Id, post)
		if err != nil {
			continue
		}

		postsIDs[u.Id] = post.Id
		names = append(names, "@"+u.Username)
	}

	return postsIDs, names, nil
}

func getAuthorizerAttachment(baseURL, id string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Actions: []*model.PostAction{
			{
				Type: "button",
				Name: "Authorize",
				Integration: &model.PostActionIntegration{
					URL: baseURL + "authorize",
					Context: map[string]interface{}{
						IDContextField: id,
					},
				},
			},
			{
				Type: "button",
				Name: "Deny",
				Integration: &model.PostActionIntegration{
					URL: baseURL + "deny",
					Context: map[string]interface{}{
						IDContextField: id,
					},
				},
			},
		},
	}
}

func getModifyierAttachment(baseURL, id string) *model.SlackAttachment {
	return &model.SlackAttachment{
		Actions: []*model.PostAction{{
			Type: "button",
			Name: "Cancel",
			Integration: &model.PostActionIntegration{
				URL: baseURL + "cancel",
				Context: map[string]interface{}{
					IDContextField: id,
				},
			},
		}},
	}
}

func (p *Plugin) updatePosts(a Authorization) error {
	users := []*model.User{}
	authorizedNames := []string{}
	toAuthorizeNames := []string{}
	for k, _ := range a.AuthorizerPosts {
		u, err := p.mm.User.Get(k)
		if err != nil {
			return err
		}
		users = append(users, u)
		if a.hasAuthorized(k) {
			authorizedNames = append(authorizedNames, "@"+u.Username)
		} else {
			toAuthorizeNames = append(toAuthorizeNames, "@"+u.Username)
		}
	}

	modifyier, err := p.mm.User.Get(a.ModifyierUserID)
	if err != nil {
		return err
	}

	for _, u := range users {
		post, err := p.mm.Post.GetPost(a.AuthorizerPosts[u.Id])
		if err != nil {
			continue
		}

		post.Message = a.getAuthorizerText(modifyier.Username, authorizedNames)
		attachments := []*model.SlackAttachment{}
		if !a.hasAuthorized(u.Id) {
			attachments = append(attachments, getAuthorizerAttachment(p.getAPIURL(), a.ID))
		}
		model.ParseSlackAttachment(post, attachments)
		_ = p.mm.Post.UpdatePost(post)
	}

	post, err := p.mm.Post.GetPost(a.ModifyierPost)
	if err != nil {
		return nil
	}

	post.Message = a.getModifyierText(toAuthorizeNames, authorizedNames)
	model.ParseSlackAttachment(post, []*model.SlackAttachment{getModifyierAttachment(p.getAPIURL(), a.ID)})
	_ = p.mm.Post.UpdatePost(post)

	return nil
}

func (a Authorization) hasAuthorized(userID string) bool {
	for _, v := range a.Authorizations {
		if userID == v {
			return true
		}
	}
	return false
}

func (p *Plugin) getNeededAuthorizations() int {
	// PLACEHOLDER
	return 1
}

func (p *Plugin) getAuthorizersIDs(modifyerID string) []string {
	// PLACEHOLDER
	u, _ := p.mm.User.GetByUsername("sysadmin")
	return []string{u.Id}
}
