package hcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hetznercloud/hcloud-go/hcloud"
	"github.com/hetznercloud/hcloud-go/hcloud/schema"
)

func TestStepPreValidate(t *testing.T) {
	testCases := []struct {
		name          string
		fakeSnapNames []string
		step          stepPreValidate
		wantAction    multistep.StepAction
	}{
		{
			"happy path: snapshot name is new",
			[]string{"snapshot-old"},
			stepPreValidate{
				SnapshotName: "snapshot-new",
			},
			multistep.ActionContinue,
		},
		{
			"want failure: old snapshot name",
			[]string{"snapshot-old"},
			stepPreValidate{
				SnapshotName: "snapshot-old",
			},
			multistep.ActionHalt,
		},
		{
			"old snapshot name but force flag",
			[]string{"snapshot-old"},
			stepPreValidate{
				Force:        true,
				SnapshotName: "snapshot-old",
			},
			multistep.ActionContinue,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errors := make(chan error, 1)
			state, teardown := setupStepPreValidate(errors, tc.fakeSnapNames)
			defer teardown()

			// do not output to stdout or console
			state.Put("ui", &packersdk.MockUi{})

			if action := tc.step.Run(context.Background(), state); action != tc.wantAction {
				t.Errorf("step.Run: want: %v; got: %v", tc.wantAction, action)
			}
			select {
			case err := <-errors:
				t.Fatalf("server: got: %s", err)
			default:
			}
		})
	}
}

// Configure a httptest server to return the list of fakeSnapNames.
// Report errors on the errors channel (cannot use testing.T, it runs on a different goroutine).
// Return a tuple (state, teardown) where:
// - state (containing the client) is ready to be passed to the step.Run() method.
// - teardown is a function meant to be deferred from the test.
func setupStepPreValidate(errors chan<- error, fakeSnapNames []string) (*multistep.BasicStateBag, func()) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			errors <- fmt.Errorf("fake server: reading request: %s", err)
			return
		}
		reqDump := fmt.Sprintf("fake server: request:\n%s %s\nbody: %s", r.Method, r.URL.Path, string(buf))
		if testing.Verbose() {
			fmt.Println(reqDump)
		}

		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		var response interface{}

		if r.Method == http.MethodGet && r.URL.Path == "/images" {
			w.WriteHeader(http.StatusOK)
			images := make([]schema.Image, 0, len(fakeSnapNames))
			for i, fakeDesc := range fakeSnapNames {
				img := schema.Image{
					ID:          1000 + i,
					Type:        string(hcloud.ImageTypeSnapshot),
					Description: fakeDesc,
				}
				images = append(images, img)
			}
			response = &schema.ImageListResponse{Images: images}
		}

		if response != nil {
			if err := enc.Encode(response); err != nil {
				errors <- fmt.Errorf("fake server: encoding reply: %s", err)
			}
			return
		}

		// no match: report error
		w.WriteHeader(http.StatusBadRequest)
		errors <- fmt.Errorf(reqDump)
	}))

	state := multistep.BasicStateBag{}
	client := hcloud.NewClient(hcloud.WithEndpoint(ts.URL))
	state.Put("hcloudClient", client)

	teardown := func() {
		ts.Close()
	}
	return &state, teardown
}
