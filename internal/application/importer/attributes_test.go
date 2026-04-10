package importer_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/config"
)

// --- config mock for attribute tests ---

type mockConfigRepoForAttrImport struct {
	store  map[string]interface{}
	setErr error
}

func newMockConfigRepoForAttrImport() *mockConfigRepoForAttrImport {
	return &mockConfigRepoForAttrImport{store: make(map[string]interface{})}
}

func (m *mockConfigRepoForAttrImport) Get(_ context.Context, key string) (interface{}, error) {
	return m.store[key], nil
}
func (m *mockConfigRepoForAttrImport) Set(_ context.Context, key string, value interface{}) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.store[key] = value
	return nil
}
func (m *mockConfigRepoForAttrImport) Delete(_ context.Context, key string) error {
	delete(m.store, key)
	return nil
}
func (m *mockConfigRepoForAttrImport) All(_ context.Context) ([]config.Entry, error) {
	entries := make([]config.Entry, 0, len(m.store))
	for k, v := range m.store {
		entries = append(entries, config.Entry{Key: k, Value: v})
	}
	return entries, nil
}

// --- tests ---

func TestAttrImport_Basic(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	csv := "code,label,type,required,options,group,group_label\n" +
		"color,Color,select,true,\"red,blue,green\",apparel,Apparel\n" +
		"weight,Weight,number,false,,physical,Physical\n" +
		"featured,Featured,boolean,false,,\n"

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Attributes != 3 {
		t.Errorf("Attributes = %d, want 3", result.Attributes)
	}
	if result.Groups != 2 {
		t.Errorf("Groups = %d, want 2", result.Groups)
	}
	if result.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", result.Skipped)
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none", result.Errors)
	}
	if repo.store["catalog.attributes"] == nil {
		t.Fatal("catalog.attributes not persisted")
	}
	if repo.store["catalog.attribute_groups"] == nil {
		t.Fatal("catalog.attribute_groups not persisted")
	}
}

func TestAttrImport_MissingCodeColumn(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	csv := "label,type\nColor,select\n"
	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing code column")
	}
	if !strings.Contains(err.Error(), "'code' column") {
		t.Errorf("error = %q, want mention of 'code' column", err.Error())
	}
}

func TestAttrImport_MissingLabelColumn(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	csv := "code,type\ncolor,select\n"
	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing label column")
	}
	if !strings.Contains(err.Error(), "'label' column") {
		t.Errorf("error = %q, want mention of 'label' column", err.Error())
	}
}

func TestAttrImport_MissingTypeColumn(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	csv := "code,label\ncolor,Color\n"
	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing type column")
	}
	if !strings.Contains(err.Error(), "'type' column") {
		t.Errorf("error = %q, want mention of 'type' column", err.Error())
	}
}

func TestAttrImport_EmptyCode(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	csv := "code,label,type\n,Color,select\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if result.Attributes != 0 {
		t.Errorf("Attributes = %d, want 0", result.Attributes)
	}
}

func TestAttrImport_InvalidType(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	csv := "code,label,type\ncolor,Color,dropdown\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) != 1 {
		t.Errorf("Errors = %d, want 1", len(result.Errors))
	}
}

func TestAttrImport_RequiredParsing(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	csv := "code,label,type,required\n" +
		"a,A,text,true\n" +
		"b,B,text,1\n" +
		"c,C,text,yes\n" +
		"d,D,text,false\n" +
		"e,E,text,\n"

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Attributes != 5 {
		t.Errorf("Attributes = %d, want 5", result.Attributes)
	}
}

func TestAttrImport_OptionsParsing(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	csv := "code,label,type,options\ncolor,Color,select,\"red, blue , green\"\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Attributes != 1 {
		t.Errorf("Attributes = %d, want 1", result.Attributes)
	}
}

func TestAttrImport_DuplicateCode(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	csv := "code,label,type\ncolor,Color,select\ncolor,Colour,text\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	// Later row wins.
	if result.Attributes != 1 {
		t.Errorf("Attributes = %d, want 1", result.Attributes)
	}
}

func TestAttrImport_PersistError(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	repo.setErr = fmt.Errorf("db down")
	imp := importer.NewAttributeImporter(repo)

	csv := "code,label,type\ncolor,Color,text\n"
	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error on persist failure")
	}
}

func TestAttrImport_ShortRow(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	csv := "code,label,type\ncolor\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if result.Attributes != 0 {
		t.Errorf("Attributes = %d, want 0", result.Attributes)
	}
}

func TestAttrImport_DuplicateCodeClearsOldGroup(t *testing.T) {
	repo := newMockConfigRepoForAttrImport()
	imp := importer.NewAttributeImporter(repo)

	// First row puts color in group "old", second row puts color in group "new".
	csv := "code,label,type,group,group_label\n" +
		"color,Color,select,old,Old Group\n" +
		"color,Colour,select,new,New Group\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Attributes != 1 {
		t.Errorf("Attributes = %d, want 1", result.Attributes)
	}
	// Old group should be gone (was left empty), new group should exist.
	if result.Groups != 1 {
		t.Errorf("Groups = %d, want 1", result.Groups)
	}
}
