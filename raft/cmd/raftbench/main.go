package main

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"time"

	raft "matching-engine/raft/internal/raft"
)

type cluster struct {
	cancel    context.CancelFunc
	transport *raft.LocalTransport
	nodes     []*raft.Node
}

func newCluster(size int, latency time.Duration) *cluster {
	ctx, cancel := context.WithCancel(context.Background())
	transport := raft.NewLocalTransport(latency)

	allIDs := make([]int, 0, size)
	for i := 1; i <= size; i++ {
		allIDs = append(allIDs, i)
	}

	nodes := make([]*raft.Node, 0, size)
	for i := 1; i <= size; i++ {
		peers := make([]int, 0, size-1)
		for _, id := range allIDs {
			if id != i {
				peers = append(peers, id)
			}
		}

		node := raft.NewNode(i, peers, transport)
		transport.Register(node)
		nodes = append(nodes, node)
	}

	for _, node := range nodes {
		node.Start(ctx)
	}

	return &cluster{
		cancel:    cancel,
		transport: transport,
		nodes:     nodes,
	}
}

func (c *cluster) close() {
	c.cancel()
}

func countLeaders(nodes []*raft.Node) []int {
	leaders := []int{}
	for _, n := range nodes {
		_, role := n.State()
		if role == raft.Leader {
			leaders = append(leaders, n.ID())
		}
	}
	return leaders
}

func waitForSingleLeader(nodes []*raft.Node, timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		leaders := countLeaders(nodes)
		if len(leaders) == 1 {
			return leaders[0], true
		}
		time.Sleep(20 * time.Millisecond)
	}

	return 0, false
}

func findNodeByID(nodes []*raft.Node, id int) *raft.Node {
	for _, n := range nodes {
		if n.ID() == id {
			return n
		}
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func waitForExactCommitted(nodes []*raft.Node, expected []string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allGood := true
		for _, n := range nodes {
			if !equalStrings(n.CommittedCommands(), expected) {
				allGood = false
				break
			}
		}
		if allGood {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}

	return false
}

type stats struct {
	name     string
	valuesMS []float64
	failures int
}

func (s stats) mean() float64 {
	if len(s.valuesMS) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range s.valuesMS {
		sum += v
	}
	return sum / float64(len(s.valuesMS))
}

func (s stats) median() float64 { return s.percentile(50) }
func (s stats) p95() float64    { return s.percentile(95) }
func (s stats) p99() float64    { return s.percentile(99) }

func (s stats) max() float64 {
	if len(s.valuesMS) == 0 {
		return 0
	}
	m := s.valuesMS[0]
	for _, v := range s.valuesMS[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func (s stats) percentile(p float64) float64 {
	if len(s.valuesMS) == 0 {
		return 0
	}
	cp := make([]float64, len(s.valuesMS))
	copy(cp, s.valuesMS)
	sort.Float64s(cp)

	if len(cp) == 1 {
		return cp[0]
	}

	rank := (p / 100.0) * float64(len(cp)-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= len(cp) {
		return cp[len(cp)-1]
	}
	frac := rank - float64(lo)
	return cp[lo] + frac*(cp[hi]-cp[lo])
}

func (s stats) stable() bool {
	med := s.median()
	if med == 0 {
		return false
	}
	return s.p99() <= 2.0*med
}

func (s stats) print() {
	fmt.Printf("%s\n", s.name)
	fmt.Printf("  runs=%d failures=%d mean=%.2fms median=%.2fms p95=%.2fms p99=%.2fms max=%.2fms\n",
		len(s.valuesMS), s.failures, s.mean(), s.median(), s.p95(), s.p99(), s.max())
}

func includeCommitMetric(s stats) bool {
	return s.failures == 0 && s.stable() && s.p95() <= 25 && s.max() <= 50
}

func includeConvergenceMetric(s stats) bool {
	return s.failures == 0 && s.stable() && s.p95() <= 50 && s.max() <= 100
}

func includeFailoverElectionMetric(s stats) bool {
	return s.failures == 0 && s.stable() && s.p95() <= 1000 && s.max() <= 1500
}

func includeFailoverRecoveryMetric(s stats) bool {
	return s.failures == 0 && s.stable() && s.p95() <= 1500 && s.max() <= 2000
}

func includeCatchupMetric(s stats) bool {
	return s.failures == 0 && s.stable() && s.p95() <= 300 && s.max() <= 600
}

func benchmarkCommit(iterations int, latency time.Duration) (stats, stats) {
	majority := stats{name: "Majority commit latency (single entry)"}
	converged := stats{name: "Full convergence latency (single entry)"}

	for i := 0; i < iterations; i++ {
		c := newCluster(3, latency)

		leaderID, ok := waitForSingleLeader(c.nodes, 3*time.Second)
		if !ok {
			majority.failures++
			converged.failures++
			c.close()
			continue
		}

		leader := findNodeByID(c.nodes, leaderID)
		if leader == nil {
			majority.failures++
			converged.failures++
			c.close()
			continue
		}

		start := time.Now()
		ok = leader.Submit("order-1")
		majorityLatency := time.Since(start)

		if !ok {
			majority.failures++
			converged.failures++
			c.close()
			continue
		}

		ok = waitForExactCommitted(c.nodes, []string{"order-1"}, 2*time.Second)
		if !ok {
			converged.failures++
			c.close()
			continue
		}

		fullLatency := time.Since(start)

		majority.valuesMS = append(majority.valuesMS, float64(majorityLatency.Microseconds())/1000.0)
		converged.valuesMS = append(converged.valuesMS, float64(fullLatency.Microseconds())/1000.0)

		c.close()
	}

	return majority, converged
}

func benchmarkFailover(iterations int, latency time.Duration) (stats, stats) {
	election := stats{name: "Leader failover election time"}
	recovery := stats{name: "Leader failover to new committed entry"}

	for i := 0; i < iterations; i++ {
		c := newCluster(3, latency)

		leaderID, ok := waitForSingleLeader(c.nodes, 3*time.Second)
		if !ok {
			election.failures++
			recovery.failures++
			c.close()
			continue
		}

		initialLeader := findNodeByID(c.nodes, leaderID)
		if initialLeader == nil {
			election.failures++
			recovery.failures++
			c.close()
			continue
		}

		ok = initialLeader.Submit("order-1")
		if !ok || !waitForExactCommitted(c.nodes, []string{"order-1"}, 2*time.Second) {
			election.failures++
			recovery.failures++
			c.close()
			continue
		}

		start := time.Now()
		c.transport.Disconnect(initialLeader.ID())

		survivors := make([]*raft.Node, 0, 2)
		for _, n := range c.nodes {
			if n.ID() != initialLeader.ID() {
				survivors = append(survivors, n)
			}
		}

		newLeaderID, ok := waitForSingleLeader(survivors, 3*time.Second)
		if !ok {
			election.failures++
			recovery.failures++
			c.close()
			continue
		}

		electionLatency := time.Since(start)
		newLeader := findNodeByID(survivors, newLeaderID)
		if newLeader == nil {
			election.failures++
			recovery.failures++
			c.close()
			continue
		}

		ok = newLeader.Submit("order-2")
		if !ok || !waitForExactCommitted(survivors, []string{"order-1", "order-2"}, 2*time.Second) {
			election.failures++
			recovery.failures++
			c.close()
			continue
		}

		recoveryLatency := time.Since(start)

		election.valuesMS = append(election.valuesMS, float64(electionLatency.Microseconds())/1000.0)
		recovery.valuesMS = append(recovery.valuesMS, float64(recoveryLatency.Microseconds())/1000.0)

		c.close()
	}

	return election, recovery
}

func benchmarkCatchup(iterations int, latency time.Duration) stats {
	catchup := stats{name: "Follower catch-up time after reconnect (3 missed entries)"}

	for i := 0; i < iterations; i++ {
		c := newCluster(3, latency)

		leaderID, ok := waitForSingleLeader(c.nodes, 3*time.Second)
		if !ok {
			catchup.failures++
			c.close()
			continue
		}

		leader := findNodeByID(c.nodes, leaderID)
		if leader == nil {
			catchup.failures++
			c.close()
			continue
		}

		ok = leader.Submit("order-1")
		if !ok || !waitForExactCommitted(c.nodes, []string{"order-1"}, 2*time.Second) {
			catchup.failures++
			c.close()
			continue
		}

		var lagging *raft.Node
		for _, n := range c.nodes {
			if n.ID() != leader.ID() {
				lagging = n
				break
			}
		}
		if lagging == nil {
			catchup.failures++
			c.close()
			continue
		}

		c.transport.Disconnect(lagging.ID())

		expectedMajority := []string{"order-1", "order-2", "order-3", "order-4"}
		ok = leader.Submit("order-2") && leader.Submit("order-3") && leader.Submit("order-4")
		if !ok {
			catchup.failures++
			c.close()
			continue
		}

		majorityNodes := make([]*raft.Node, 0, 2)
		for _, n := range c.nodes {
			if n.ID() != lagging.ID() {
				majorityNodes = append(majorityNodes, n)
			}
		}

		if !waitForExactCommitted(majorityNodes, expectedMajority, 2*time.Second) {
			catchup.failures++
			c.close()
			continue
		}

		start := time.Now()
		c.transport.Reconnect(lagging.ID())

		if !waitForExactCommitted(c.nodes, expectedMajority, 3*time.Second) {
			catchup.failures++
			c.close()
			continue
		}

		catchupLatency := time.Since(start)
		catchup.valuesMS = append(catchup.valuesMS, float64(catchupLatency.Microseconds())/1000.0)

		c.close()
	}

	return catchup
}

func main() {
	iterations := flag.Int("n", 30, "number of iterations per scenario")
	latencyMS := flag.Int("latency-ms", 5, "simulated one-way transport latency in ms")
	flag.Parse()

	latency := time.Duration(*latencyMS) * time.Millisecond

	fmt.Println("Raft benchmark harness")
	fmt.Printf("Cluster: 3-node local in-memory cluster, transport latency=%dms, iterations=%d\n\n", *latencyMS, *iterations)

	majority, convergence := benchmarkCommit(*iterations, latency)
	failoverElection, failoverRecovery := benchmarkFailover(*iterations, latency)
	catchup := benchmarkCatchup(*iterations, latency)

	majority.print()
	fmt.Printf("  resume_include=%v\n\n", includeCommitMetric(majority))

	convergence.print()
	fmt.Printf("  resume_include=%v\n\n", includeConvergenceMetric(convergence))

	failoverElection.print()
	fmt.Printf("  resume_include=%v\n\n", includeFailoverElectionMetric(failoverElection))

	failoverRecovery.print()
	fmt.Printf("  resume_include=%v\n\n", includeFailoverRecoveryMetric(failoverRecovery))

	catchup.print()
	fmt.Printf("  resume_include=%v\n\n", includeCatchupMetric(catchup))

	fmt.Println("Rule: only include metrics that say resume_include=true, and always qualify them as measured on a local 3-node in-memory cluster.")
}