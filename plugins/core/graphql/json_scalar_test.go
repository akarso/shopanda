package graphql

import (
	"errors"
	"fmt"
	"testing"

	"github.com/graphql-go/graphql/language/ast"

	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestParseJSONLiteral(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		got := parseJSONLiteral(&ast.StringValue{Value: "hello"})
		if got != "hello" {
			t.Fatalf("got %#v, want hello", got)
		}
	})
	t.Run("int", func(t *testing.T) {
		got := parseJSONLiteral(&ast.IntValue{Value: "42"})
		if got != 42 {
			t.Fatalf("got %#v, want 42", got)
		}
	})
	t.Run("float", func(t *testing.T) {
		got := parseJSONLiteral(&ast.FloatValue{Value: "3.14"})
		if got != 3.14 {
			t.Fatalf("got %#v, want 3.14", got)
		}
	})
	t.Run("bool", func(t *testing.T) {
		got := parseJSONLiteral(&ast.BooleanValue{Value: true})
		if got != true {
			t.Fatalf("got %#v, want true", got)
		}
	})
	t.Run("list", func(t *testing.T) {
		got := parseJSONLiteral(&ast.ListValue{
			Values: []ast.Value{
				&ast.StringValue{Value: "a"},
				&ast.IntValue{Value: "1"},
			},
		})
		want := []interface{}{"a", 1}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("object", func(t *testing.T) {
		got := parseJSONLiteral(&ast.ObjectValue{
			Fields: []*ast.ObjectField{
				{Name: &ast.Name{Value: "foo"}, Value: &ast.StringValue{Value: "bar"}},
				{Name: &ast.Name{Value: "n"}, Value: &ast.IntValue{Value: "7"}},
			},
		})
		want := map[string]interface{}{"foo": "bar", "n": 7}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("variable returns nil", func(t *testing.T) {
		if got := parseJSONLiteral(&ast.Variable{Name: &ast.Name{Value: "v"}}); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
}

func TestExtensionAPIErrorWrappedSentinels(t *testing.T) {
	cases := []struct {
		name string
		in   error
		code apperror.Code
	}{
		{
			name: "unknown field code",
			in:   fmt.Errorf("upsert: %w", domainext.ErrUnknownFieldCode),
			code: apperror.CodeUnknownFieldCode,
		},
		{
			name: "forbidden private field",
			in:   fmt.Errorf("delete: %w", domainext.ErrForbiddenPrivateField),
			code: apperror.CodeForbiddenPrivateField,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := extensionAPIError(tc.in)
			var appErr *apperror.Error
			if !errors.As(err, &appErr) {
				t.Fatalf("expected apperror.Error, got %T (%v)", err, err)
			}
			if appErr.Code != tc.code {
				t.Fatalf("code = %q, want %q", appErr.Code, tc.code)
			}
		})
	}
}
