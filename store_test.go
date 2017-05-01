package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/mdigger/rest"
)

func TestStore(t *testing.T) {
	s, err := OpenStore("test.db")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		dbname := s.db.Path()
		if err := s.Close(); err != nil {
			t.Error(err)
		}
		if err := os.Remove(dbname); err != nil {
			t.Error(err)
		}
	}()

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "    ")

	err = s.Save("section", map[string]interface{}{
		"service1": rest.JSON{"name": "service 1", "counter": 1},
		"service2": rest.JSON{"name": "service 2", "counter": 2},
		"service3": rest.JSON{"name": "service 3", "counter": 3},
		"service4": rest.JSON{"name": "service 4", "counter": 4},
		"service5": nil,
	})
	if err != nil {
		t.Error(err)
	}

	list, err := s.Keys("section")
	if err != nil {
		t.Error(err)
	}
	enc.Encode(list)

	err = s.Remove("section", "service4", "service6")
	if err != nil {
		t.Error(err)
	}

	values, err := s.Get("section", "service1", "service2", "service3", "service4", "service5")
	if err != nil {
		t.Error(err)
	}
	enc.Encode(values)
}
