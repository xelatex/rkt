// Copyright 2015 The rkt Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"fmt"
	"syscall"
	"testing"

	"github.com/coreos/rkt/Godeps/_workspace/src/github.com/coreos/gexpect"
	"github.com/coreos/rkt/Godeps/_workspace/src/golang.org/x/net/context"
	"github.com/coreos/rkt/api/v1alpha"
	"github.com/coreos/rkt/common"
	"github.com/coreos/rkt/tests/testutils"
)

func startAPIService(t *testing.T, ctx *testutils.RktRunCtx) *gexpect.ExpectSubprocess {
	noGid := false
	gid, err := common.LookupGid(common.RktGroup)
	if err != nil {
		t.Logf("no %q group, will run api service with root, ONLY DO THIS FOR TESTING!", common.RktGroup)
		noGid = true
	} else {
		t.Logf("Running rkt install")
		installCmd := fmt.Sprintf("%s install", ctx.Cmd())
		runRktAndCheckOutput(t, installCmd, "rkt directory structure successfully created", false)
	}

	t.Logf("Running rkt api service")
	apisvcCmd := fmt.Sprintf("%s api-service", ctx.Cmd())

	if noGid {
		return startRktAndCheckOutput(t, apisvcCmd, "API service running")
	}
	return startRktAsGidAndCheckOutput(t, apisvcCmd, "API service running", gid)
}

func stopAPIService(t *testing.T, svc *gexpect.ExpectSubprocess) {
	if err := svc.Cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("Failed to stop the api service: %v", err)
	}
	waitOrFail(t, svc, true)
}

func checkPodState(t *testing.T, rawState string, apiState v1alpha.PodState) {
	switch rawState {
	case "embryo":
		if apiState == v1alpha.PodState_POD_STATE_EMBRYO {
			return
		}
	case "preparing":
		if apiState == v1alpha.PodState_POD_STATE_PREPARING {
			return
		}
	case "aborted prepare":
		if apiState == v1alpha.PodState_POD_STATE_ABORTED_PREPARE {
			return
		}
	case "running":
		if apiState == v1alpha.PodState_POD_STATE_RUNNING {
			return
		}
	case "deleting":
		if apiState == v1alpha.PodState_POD_STATE_DELETING {
			return
		}
	case "exited":
		if apiState == v1alpha.PodState_POD_STATE_EXITED {
			return
		}
	case "garbage":
		if apiState == v1alpha.PodState_POD_STATE_GARBAGE {
			return
		}
	default:
		t.Fatalf("Unexpected state: %v", rawState)
	}
	t.Errorf("Pod state returned by api-service (%q) is not equivalent to the state returned by 'rkt status' (%q)", apiState, rawState)
}

func checkPodApps(t *testing.T, rawApps map[string]*appInfo, apiApps []*v1alpha.App, hasAppState bool) {
	if len(rawApps) != len(apiApps) {
		t.Errorf("Expected %d apps, saw %d apps returned by api service %v", len(rawApps), len(apiApps), apiApps)
	}

	for _, app := range apiApps {
		if appInfo, ok := rawApps[app.Name]; ok {
			if hasAppState && appInfo.exitCode != int(app.ExitCode) {
				t.Errorf("Expected %v, saw %v", appInfo.exitCode, app.ExitCode)
			}
			if appInfo.image.id != app.Image.Id {
				t.Errorf("Expected %q, saw %q", appInfo.image.id, app.Image.Id)
			}
		} else {
			t.Errorf("Expected app (name: %q) in the app list", appInfo.name)
		}
	}
}

func checkPodNetworks(t *testing.T, rawNets map[string]*networkInfo, apiNets []*v1alpha.Network) {
	if len(rawNets) != len(apiNets) {
		t.Errorf("Expected %d networks, saw %d networks returned by api service", len(rawNets), len(apiNets))
	}

	// Each network should have a unique name, so iteration over one list is enough given
	// the lengths of the two lists are equal.
	for _, net := range apiNets {
		if netInfo, ok := rawNets[net.Name]; ok {
			if netInfo.ipv4 != net.Ipv4 {
				t.Errorf("Expected %q, saw %q", netInfo.ipv4, net.Ipv4)
			}
		} else {
			t.Errorf("Expected network (name: %q, ipv4: %q) in networks", netInfo.name, netInfo.ipv4)
		}
	}
}

// Check the pod's information by 'rkt status'.
func checkPod(t *testing.T, ctx *testutils.RktRunCtx, p *v1alpha.Pod, hasAppState, hasManifest bool) {
	podInfo := getPodInfo(t, ctx, p.Id)
	if podInfo.id != p.Id {
		t.Errorf("Expected %q, saw %q", podInfo.id, p.Id)
	}
	if podInfo.pid != int(p.Pid) {
		t.Errorf("Expected %d, saw %d", podInfo.pid, p.Pid)
	}
	checkPodState(t, podInfo.state, p.State)
	checkPodApps(t, podInfo.apps, p.Apps, hasAppState)
	checkPodNetworks(t, podInfo.networks, p.Networks)

	if hasManifest && !bytes.Equal(podInfo.manifest, p.Manifest) {
		t.Errorf("Expected %q, saw %q", string(podInfo.manifest), string(p.Manifest))
	} else if !hasManifest && p.Manifest != nil {
		t.Errorf("Expected nil manifest")
	}
}

func checkPodBasics(t *testing.T, ctx *testutils.RktRunCtx, p *v1alpha.Pod) {
	checkPod(t, ctx, p, false, false)
}

func checkPodDetails(t *testing.T, ctx *testutils.RktRunCtx, p *v1alpha.Pod) {
	checkPod(t, ctx, p, true, true)
}

// Check the image's information by 'rkt image list'.
func checkImage(t *testing.T, ctx *testutils.RktRunCtx, m *v1alpha.Image, hasManifest bool) {
	imgInfo := getImageInfo(t, ctx, m.Id)
	if imgInfo.id != m.Id {
		t.Errorf("Expected %q, saw %q", imgInfo.id, m.Id)
	}
	if imgInfo.name != m.Name {
		t.Errorf("Expected %q, saw %q", imgInfo.name, m.Name)
	}
	if imgInfo.version != m.Version {
		t.Errorf("Expected %q, saw %q", imgInfo.version, m.Version)
	}
	if imgInfo.importTime != m.ImportTimestamp {
		t.Errorf("Expected %q, saw %q", imgInfo.importTime, m.ImportTimestamp)
	}
	if imgInfo.size != m.Size {
		t.Errorf("Expected size %d, saw %d", imgInfo.size, m.Size)
	}

	if hasManifest && !bytes.Equal(imgInfo.manifest, m.Manifest) {
		t.Errorf("Expected %q, saw %q", string(imgInfo.manifest), string(m.Manifest))
	} else if !hasManifest && m.Manifest != nil {
		t.Errorf("Expected nil manifest")
	}
}

func checkImageBasics(t *testing.T, ctx *testutils.RktRunCtx, m *v1alpha.Image) {
	checkImage(t, ctx, m, false)
}

func checkImageDetails(t *testing.T, ctx *testutils.RktRunCtx, m *v1alpha.Image) {
	checkImage(t, ctx, m, true)
}

func TestAPIServiceGetInfo(t *testing.T) {
	ctx := testutils.NewRktRunCtx()
	defer ctx.Cleanup()

	svc := startAPIService(t, ctx)
	defer stopAPIService(t, svc)

	c, conn := newAPIClientOrFail(t, "localhost:15441")
	defer conn.Close()

	resp, err := c.GetInfo(context.Background(), &v1alpha.GetInfoRequest{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expectedAPIVersion := "1.0.0-alpha"
	if resp.Info.ApiVersion != expectedAPIVersion {
		t.Errorf("Expected api version to be %q, but saw %q", expectedAPIVersion, resp.Info.ApiVersion)
	}
}

func TestAPIServiceListInspectPods(t *testing.T) {
	ctx := testutils.NewRktRunCtx()
	defer ctx.Cleanup()

	svc := startAPIService(t, ctx)
	defer stopAPIService(t, svc)

	c, conn := newAPIClientOrFail(t, "localhost:15441")
	defer conn.Close()

	resp, err := c.ListPods(context.Background(), &v1alpha.ListPodsRequest{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(resp.Pods) != 0 {
		t.Errorf("Unexpected result: %v, should see zero pods", resp.Pods)
	}

	patchImportAndRun("rkt-inspect-print.aci", []string{"--exec=/inspect --print-msg=HELLO_API --exit-code=42"}, t, ctx)

	// ListPods(detail=false).
	resp, err = c.ListPods(context.Background(), &v1alpha.ListPodsRequest{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(resp.Pods) == 0 {
		t.Errorf("Unexpected result: %v, should see non-zero pods", resp.Pods)
	}

	for _, p := range resp.Pods {
		checkPodBasics(t, ctx, p)

		// Test InspectPod().
		inspectResp, err := c.InspectPod(context.Background(), &v1alpha.InspectPodRequest{Id: p.Id})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		checkPodDetails(t, ctx, inspectResp.Pod)
	}

	// ListPods(detail=true).
	resp, err = c.ListPods(context.Background(), &v1alpha.ListPodsRequest{Detail: true})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(resp.Pods) == 0 {
		t.Errorf("Unexpected result: %v, should see non-zero pods", resp.Pods)
	}

	for _, p := range resp.Pods {
		checkPodDetails(t, ctx, p)
	}
}

func TestAPIServiceListInspectImages(t *testing.T) {
	ctx := testutils.NewRktRunCtx()
	defer ctx.Cleanup()

	svc := startAPIService(t, ctx)
	defer stopAPIService(t, svc)

	c, conn := newAPIClientOrFail(t, "localhost:15441")
	defer conn.Close()

	resp, err := c.ListImages(context.Background(), &v1alpha.ListImagesRequest{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(resp.Images) != 0 {
		t.Errorf("Unexpected result: %v, should see zero images", resp.Images)
	}

	patchImportAndFetchHash("rkt-inspect-sleep.aci", []string{"--exec=/inspect"}, t, ctx)

	// ListImages(detail=false).
	resp, err = c.ListImages(context.Background(), &v1alpha.ListImagesRequest{})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(resp.Images) == 0 {
		t.Errorf("Unexpected result: %v, should see non-zero images", resp.Images)
	}

	for _, m := range resp.Images {
		checkImageBasics(t, ctx, m)

		// Test InspectImage().
		inspectResp, err := c.InspectImage(context.Background(), &v1alpha.InspectImageRequest{Id: m.Id})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		checkImageDetails(t, ctx, inspectResp.Image)
	}

	// ListImages(detail=true).
	resp, err = c.ListImages(context.Background(), &v1alpha.ListImagesRequest{Detail: true})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(resp.Images) == 0 {
		t.Errorf("Unexpected result: %v, should see non-zero images", resp.Images)
	}

	for _, m := range resp.Images {
		checkImageDetails(t, ctx, m)
	}
}
