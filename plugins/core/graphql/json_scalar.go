package graphql

import (
	"math"
	"strconv"

	gql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

func newJSONScalar() *gql.Scalar {
	return gql.NewScalar(gql.ScalarConfig{
		Name: "JSON",
		Serialize: func(v interface{}) interface{} {
			return v
		},
		ParseValue: func(v interface{}) interface{} {
			return v
		},
		ParseLiteral: func(vAST ast.Value) interface{} {
			return parseJSONLiteral(vAST)
		},
	})
}

func parseJSONLiteral(valueAST ast.Value) interface{} {
	if valueAST == nil {
		return nil
	}
	switch v := valueAST.(type) {
	case *ast.Variable:
		return nil
	case *ast.IntValue:
		i, err := strconv.ParseInt(v.Value, 10, 64)
		if err != nil {
			return nil
		}
		if i >= int64(math.MinInt) && i <= int64(math.MaxInt) {
			return int(i)
		}
		return i
	case *ast.FloatValue:
		f, err := strconv.ParseFloat(v.Value, 64)
		if err != nil {
			return nil
		}
		return f
	case *ast.StringValue:
		return v.Value
	case *ast.BooleanValue:
		return v.Value
	case *ast.EnumValue:
		return v.Value
	case *ast.ListValue:
		out := make([]interface{}, 0, len(v.Values))
		for _, item := range v.Values {
			out = append(out, parseJSONLiteral(item))
		}
		return out
	case *ast.ObjectValue:
		out := make(map[string]interface{}, len(v.Fields))
		for _, field := range v.Fields {
			if field == nil || field.Name == nil {
				continue
			}
			out[field.Name.Value] = parseJSONLiteral(field.Value)
		}
		return out
	default:
		return nil
	}
}
