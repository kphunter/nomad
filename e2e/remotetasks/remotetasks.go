package remotetasks

import (
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/e2e/e2eutil"
	"github.com/hashicorp/nomad/e2e/framework"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	ecsTaskStatusDeactivating   = "DEACTIVATING"
	ecsTaskStatusStopping       = "STOPPING"
	ecsTaskStatusDeprovisioning = "DEPROVISIONING"
	ecsTaskStatusStopped        = "STOPPED"
	ecsTaskStatusRunning        = "RUNNING"
)

type RemoteTasksTest struct {
	framework.TC
	jobIDs []string
}

func init() {
	framework.AddSuites(&framework.TestSuite{
		Component:   "RemoteTasks",
		CanRunLocal: true,
		Cases: []framework.TestCase{
			new(RemoteTasksTest),
		},
	})
}

func (tc *RemoteTasksTest) BeforeAll(f *framework.F) {
	e2eutil.WaitForLeader(f.T(), tc.Nomad())
	e2eutil.WaitForNodesReady(f.T(), tc.Nomad(), 2)
}

func (tc *RemoteTasksTest) AfterEach(f *framework.F) {
	nomadClient := tc.Nomad()

	// Mark all nodes eligible
	nodesAPI := tc.Nomad().Nodes()
	nodes, _, _ := nodesAPI.List(nil)
	for _, node := range nodes {
		nodesAPI.ToggleEligibility(node.ID, true, nil)
	}

	jobs := nomadClient.Jobs()
	// Stop all jobs in test
	for _, id := range tc.jobIDs {
		jobs.Deregister(id, true, nil)
	}
	tc.jobIDs = []string{}

	// Garbage collect
	nomadClient.System().GarbageCollect()
}

// TestECSJob asserts an ECS job may be started and is cleaned up when stopped.
func (tc *RemoteTasksTest) TestECSJob(f *framework.F) {
	t := f.T()

	ecsClient := ecsOrSkip(t)

	jobID := "ecsjob-" + uuid.Generate()[0:8]
	tc.jobIDs = append(tc.jobIDs, jobID)
	allocs := e2eutil.RegisterAndWaitForAllocs(t, tc.Nomad(), "remotetasks/input/ecs.nomad", jobID, "")
	require.Len(t, allocs, 1)
	allocID := allocs[0].ID
	e2eutil.WaitForAllocsRunning(t, tc.Nomad(), []string{allocID})

	// We need to go from Allocation -> ECS ARN, so grab the updated
	// allocation's task state.
	arn := arnForAlloc(t, tc.Nomad().Allocations(), allocID)

	// Use ARN to lookup status of ECS task in AWS
	ensureECSRunning(t, ecsClient, arn)

	//TODO(schmichael) - remove?
	t.Logf("Task %s is running!", arn)

	// Stop the job
	e2eutil.WaitForJobStopped(t, tc.Nomad(), jobID)

	// Ensure it is stopped in ECS
	input := ecs.DescribeTasksInput{
		//TODO(schmichael) - How to determine cluster? Hardcode?
		Cluster: aws.String("nomad-rtd-e2e"),
		Tasks:   []*string{aws.String(arn)},
	}
	testutil.WaitForResult(func() (bool, error) {
		resp, err := ecsClient.DescribeTasks(&input)
		if err != nil {
			return false, err
		}
		status := *resp.Tasks[0].LastStatus
		return status == ecsTaskStatusStopped, fmt.Errorf("ecs task is not stopped: %s", status)
	}, func(err error) {
		t.Fatalf("error retrieving ecs task status: %v", err)
	})
}

// TestECSDrain asserts an ECS job may be started, drained from one node, and
// is managed by a new node without stopping and restarting the remote task.
func (tc *RemoteTasksTest) TestECSDrain(f *framework.F) {
	t := f.T()

	ecsClient := ecsOrSkip(t)

	jobID := "ecsjob-" + uuid.Generate()[0:8]
	tc.jobIDs = append(tc.jobIDs, jobID)
	allocs := e2eutil.RegisterAndWaitForAllocs(t, tc.Nomad(), "remotetasks/input/ecs.nomad", jobID, "")
	require.Len(t, allocs, 1)
	origNode := allocs[0].NodeID
	origAlloc := allocs[0].ID
	e2eutil.WaitForAllocsRunning(t, tc.Nomad(), []string{origAlloc})

	arn := arnForAlloc(t, tc.Nomad().Allocations(), origAlloc)
	ensureECSRunning(t, ecsClient, arn)

	//TODO(schmichael) - remove?
	t.Logf("Task %s is running! Now to drain the node.", arn)

	// Drain the node
	_, err := tc.Nomad().Nodes().UpdateDrain(
		origNode,
		&api.DrainSpec{Deadline: 30 * time.Second},
		false,
		nil,
	)
	require.NoError(t, err, "error draining original node")

	// Wait for new alloc to be running
	var newAlloc *api.AllocationListStub
	qopts := &api.QueryOptions{}
	testutil.WaitForResult(func() (bool, error) {
		allocs, resp, err := tc.Nomad().Jobs().Allocations(jobID, false, qopts)
		if err != nil {
			return false, fmt.Errorf("error retrieving allocations for job: %w", err)
		}

		qopts.WaitIndex = resp.LastIndex

		if len(allocs) > 2 {
			return false, fmt.Errorf("expected 1 or 2 allocs but found %d", len(allocs))
		}

		for _, alloc := range allocs {
			if alloc.ID == origAlloc {
				// This is the old alloc, skip it
				continue
			}

			newAlloc = alloc

			if newAlloc.ClientStatus == "running" {
				break
			}
		}

		if newAlloc == nil {
			return false, fmt.Errorf("no new alloc found")
		}
		if newAlloc.ClientStatus != "running" {
			return false, fmt.Errorf("expected new alloc (%s) to be running but found: %s",
				newAlloc.ID, newAlloc.ClientStatus)
		}

		return true, nil
	}, func(err error) {
		t.Fatalf("error waiting for new alloc to be running: %v", err)
	})

	//TODO: make sure the ARN hasn't changed by looking up the new alloc's ARN
	newARN := arnForAlloc(t, tc.Nomad().Allocations(), newAlloc.ID)

	assert.NotEqual(t, arn, newARN, "unexpected new ARN")
}

// ecsOrSkip returns an AWS ECS client or skips the test if ECS is unreachable.
func ecsOrSkip(t *testing.T) *ecs.ECS {
	awsSession := session.Must(session.NewSession())

	//TODO(schmichael) - How to determine the region?
	ecsClient := ecs.New(awsSession, aws.NewConfig().WithRegion("us-east-1"))

	_, err := ecsClient.ListClusters(&ecs.ListClustersInput{})
	if err != nil {
		t.Skipf("Skipping ECS Remote Task Driver Task. Error querying AWS ECS API: %v", err)
	}

	return ecsClient
}

// arnForAlloc retrieves the ARN for a running allocation.
func arnForAlloc(t *testing.T, allocAPI *api.Allocations, allocID string) string {
	t.Logf("Retrieving ARN for alloc=%s", allocID)
	ecsState := struct {
		ARN string
	}{}
	testutil.WaitForResult(func() (bool, error) {
		alloc, _, err := allocAPI.Info(allocID, nil)
		if err != nil {
			return false, err
		}
		state := alloc.TaskStates["http-server"]
		if state == nil {
			return false, fmt.Errorf("no task state for http-server (%d task states)", len(alloc.TaskStates))
		}
		if state.TaskHandle == nil {
			return false, fmt.Errorf("no task handle for http-server")
		}
		if len(state.TaskHandle.DriverState) == 0 {
			return false, fmt.Errorf("no driver state for task handle")
		}
		if err := base.MsgPackDecode(state.TaskHandle.DriverState, &ecsState); err != nil {
			return false, fmt.Errorf("error decoding driver state: %w", err)
		}
		if ecsState.ARN == "" {
			return false, fmt.Errorf("ARN is empty despite DriverState being %d bytes", len(state.TaskHandle.DriverState))
		}
		return true, nil
	}, func(err error) {
		t.Fatalf("error getting ARN: %v", err)
	})
	t.Logf("Retrieved ARN=%s for alloc=%s", arn, allocID)

	return ecsState.ARN
}

// ensureECSRunning asserts that the given ARN is a running ECS task.
func ensureECSRunning(t *testing.T, ecsClient *ecs.ECS, arn string) {
	t.Logf("Ensuring ARN=%s is running", arn)
	input := ecs.DescribeTasksInput{
		//TODO(schmichael) - How to determine cluster? Hardcode?
		Cluster: aws.String("nomad-rtd-e2e"),
		Tasks:   []*string{aws.String(arn)},
	}
	testutil.WaitForResult(func() (bool, error) {
		resp, err := ecsClient.DescribeTasks(&input)
		if err != nil {
			return false, err
		}
		status := *resp.Tasks[0].LastStatus
		return status == ecsTaskStatusRunning, fmt.Errorf("ecs task is not running: %s", status)
	}, func(err error) {
		t.Fatalf("error retrieving ecs task status: %v", err)
	})
	t.Logf("ARN=%s is running", arn)
}
