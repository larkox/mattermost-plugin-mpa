package main

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/mattermost/mattermost-server/v6/model"
	"github.com/mattermost/mattermost-server/v6/plugin"
)

func (p *Plugin) getAutocomplete() *model.AutocompleteData {
	baseACD := model.NewAutocompleteData("mpa", "", "Change configuration through Multi-party Authorization")
	conf := p.API.GetConfig()

	cType := reflect.TypeOf(*conf)
	baseACD = p.recGetAutocomplete(baseACD, cType)
	return baseACD
}

func (p *Plugin) recGetAutocomplete(base *model.AutocompleteData, typeDef reflect.Type) *model.AutocompleteData {
	for i := 0; i < typeDef.NumField(); i++ {
		field := typeDef.Field(i)
		acd := model.NewAutocompleteData(strings.ToLower(field.Name), "", getCommandDescription(field.Type))
		base.AddCommand(acd)

		if field.Type.Kind() == reflect.Struct {
			p.recGetAutocomplete(acd, field.Type)
		}
	}

	return base
}

func getCommandDescription(typeDef reflect.Type) string {
	out := ""
	if typeDef.Name() != "" {
		out = typeDef.Name() + " - "
	}

	if typeDef.Kind() != reflect.Ptr {
		return out + typeDef.Kind().String()
	}

	return out + "*" + typeDef.Elem().Kind().String()
}

func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	split := strings.Fields(args.Command)
	command := split[0]
	var rest []string
	if len(split) > 2 {
		rest = split[1:]
	}

	if command != "/mpa" {
		return &model.CommandResponse{}, nil
	}

	conf := p.API.GetConfig()

	return p.recExecuteCommand(rest, []string{"conf"}, *conf, args.UserId), nil
}

func (p *Plugin) recExecuteCommand(params []string, fields []string, conf interface{}, userID string) *model.CommandResponse {
	fieldName := fields[len(fields)-1]
	reflected := reflect.ValueOf(conf)
	typeDef := reflected.Type()

	if len(params) == 0 {
		if typeDef.Kind() == reflect.Struct {
			return errOut(fmt.Sprintf("%s is not a leaf config value. Cannot show the value", fieldName))
		}
		value := conf
		if typeDef.Kind() == reflect.Ptr {
			value = reflected.Elem()
		}
		return commandOut(fmt.Sprintf("The value of %s is %v", fieldName, value))
	}

	if typeDef.Kind() != reflect.Struct {
		value := strings.Join(params, " ")
		valueType := typeDef
		if typeDef.Kind() == reflect.Ptr {
			valueType = typeDef.Elem()
		}
		switch valueType.Kind() {
		case reflect.String:
			// No problem
		case reflect.Bool:
			if value != "true" && value != "false" {
				return errOut(fmt.Sprintf("Bool values must be either `true` or `false`. `%s` is not a valid value", value))
			}
		case reflect.Int:
			_, err := strconv.Atoi(value)
			if err != nil {
				return errOut(fmt.Sprintf("Cannot parse `%s` as int value. Error: %v", value, err))
			}
		case reflect.Int64:
			_, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return errOut(fmt.Sprintf("Cannot parse `%s` as int64. Error: %v", value, err))
			}
		default:
			return errOut(fmt.Sprintf("Type %s not supported by MPA.", valueType.Kind()))
		}
		p.StartMPA(Authorization{
			Field:           fields,
			Value:           value,
			ModifyierUserID: userID,
		})
		return commandOut(fmt.Sprintf("Process to assign `%s` to %s has started. Check your DMs.", value, fieldName))
	}

	current := params[0]
	rest := params[1:]

	field, found := typeDef.FieldByNameFunc(func(s string) bool { return strings.ToLower(s) == current })
	if !found {
		return errOut(fmt.Sprintf("Field %s has no field %s", fieldName, current))
	}

	return p.recExecuteCommand(rest, append(fields, field.Name), reflected.FieldByName(field.Name).Interface(), userID)
}

func errOut(s string) *model.CommandResponse {
	return &model.CommandResponse{
		Text: s,
	}
}

func commandOut(s string) *model.CommandResponse {
	return &model.CommandResponse{
		Text: s,
	}
}
