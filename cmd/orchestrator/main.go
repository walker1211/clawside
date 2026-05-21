package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/walker1211/clawside/internal/orchestrator"

	_ "modernc.org/sqlite"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing command")
	}
	switch args[0] {
	case "handoff":
		return runHandoff(args[1:], stdout, stderr)
	case "event":
		return runEvent(args[1:], stdout, stderr)
	case "signal":
		return runSignal(args[1:], stdout, stderr)
	case "workflow":
		return runWorkflow(args[1:], stdout, stderr)
	case "watch":
		return runWatch(args[1:], stdout, stderr)
	case "repair":
		return runRepair(args[1:], stdout, stderr)
	case "repair-candidate":
		return runRepairCandidate(args[1:], stdout, stderr)
	case "divergence":
		return runDivergence(args[1:], stdout, stderr)
	case "ownership":
		return runOwnership(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runHandoff(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing handoff subcommand")
	}
	switch args[0] {
	case "create":
		return runHandoffCreate(args[1:], stdout, stderr)
	case "get":
		return runHandoffGet(args[1:], stdout, stderr)
	case "list":
		return runHandoffList(args[1:], stdout, stderr)
	case "dispatch":
		return runHandoffDispatch(args[1:], stdout, stderr)
	case "timeline":
		return runHandoffTimeline(args[1:], stdout, stderr)
	case "receive":
		return runHandoffProtocolAction(args[1:], stdout, stderr, orchestrator.ProtocolActionReceive)
	case "claim":
		return runHandoffProtocolAction(args[1:], stdout, stderr, orchestrator.ProtocolActionClaim)
	case "start":
		return runHandoffProtocolAction(args[1:], stdout, stderr, orchestrator.ProtocolActionStart)
	case "checkpoint":
		return runHandoffProtocolAction(args[1:], stdout, stderr, orchestrator.ProtocolActionCheckpoint)
	case "submit":
		return runHandoffProtocolAction(args[1:], stdout, stderr, orchestrator.ProtocolActionSubmit)
	case "review":
		return runHandoffProtocolAction(args[1:], stdout, stderr, orchestrator.ProtocolActionReview)
	case "request-revision":
		return runHandoffProtocolAction(args[1:], stdout, stderr, orchestrator.ProtocolActionRequestRevision)
	case "approve":
		return runHandoffProtocolAction(args[1:], stdout, stderr, orchestrator.ProtocolActionApprove)
	case "complete":
		return runHandoffProtocolAction(args[1:], stdout, stderr, orchestrator.ProtocolActionComplete)
	case "fail":
		return runHandoffProtocolAction(args[1:], stdout, stderr, orchestrator.ProtocolActionFail)
	default:
		return fmt.Errorf("unknown handoff subcommand %q", args[0])
	}
}

func runEvent(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing event subcommand")
	}
	switch args[0] {
	case "record":
		return runEventRecord(args[1:], stdout, stderr)
	case "list":
		return runEventList(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown event subcommand %q", args[0])
	}
}

func runSignal(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing signal subcommand")
	}
	switch args[0] {
	case "record":
		return runSignalRecord(args[1:], stdout, stderr)
	case "list":
		return runSignalList(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown signal subcommand %q", args[0])
	}
}

func runDivergence(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing divergence subcommand")
	}
	switch args[0] {
	case "list":
		return runDivergenceList(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown divergence subcommand %q", args[0])
	}
}

func runWorkflow(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing workflow subcommand")
	}
	switch args[0] {
	case "status":
		return runWorkflowStatus(args[1:], stdout, stderr)
	case "list":
		return runWorkflowList(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown workflow subcommand %q", args[0])
	}
}

func runWatch(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing watch subcommand")
	}
	switch args[0] {
	case "list":
		return runWatchList(args[1:], stdout, stderr)
	case "run":
		return runWatchRun(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown watch subcommand %q", args[0])
	}
}

func runRepair(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing repair subcommand")
	}
	switch args[0] {
	case "invalidate-event":
		return runRepairInvalidateEvent(args[1:], stdout, stderr)
	case "reopen-handoff":
		return runRepairReopenHandoff(args[1:], stdout, stderr)
	case "backfill-event":
		return runRepairBackfillEvent(args[1:], stdout, stderr)
	case "list":
		return runRepairList(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown repair subcommand %q", args[0])
	}
}

func runRepairCandidate(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing repair-candidate subcommand")
	}
	switch args[0] {
	case "list":
		return runRepairCandidateList(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown repair-candidate subcommand %q", args[0])
	}
}

func runOwnership(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("missing ownership subcommand")
	}
	switch args[0] {
	case "get":
		return runOwnershipGet(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown ownership subcommand %q", args[0])
	}
}

func runHandoffCreate(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("handoff create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var dbPath string
	var workflowKind string
	var sender string
	var receiver string
	var taskKind string
	var intent string
	var parentHandoffID string
	var dependsOn string
	var workflowID string
	var payloadRef string
	var deliveryTargetRef string
	var required bool

	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&workflowKind, "workflow-kind", "", "workflow kind")
	fs.StringVar(&workflowID, "workflow-id", "", "existing workflow id for appending a handoff")
	fs.StringVar(&sender, "sender", "", "sender actor")
	fs.StringVar(&receiver, "receiver", "", "receiver actor")
	fs.StringVar(&taskKind, "task-kind", string(orchestrator.TaskGeneric), "task kind")
	fs.StringVar(&intent, "intent", "", "intent")
	fs.StringVar(&parentHandoffID, "parent-handoff-id", "", "parent handoff id")
	fs.StringVar(&dependsOn, "depends-on", "", "depends on handoff ids")
	fs.StringVar(&payloadRef, "payload-ref", "", "payload or project reference")
	fs.StringVar(&deliveryTargetRef, "delivery-target-ref", "", "delivery target reference")
	fs.BoolVar(&required, "required-for-workflow-completion", false, "required for workflow completion")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)

	senderRef, err := parseActorRef(sender)
	if err != nil {
		return err
	}
	receiverRef, err := parseActorRef(receiver)
	if err != nil {
		return err
	}
	var parent *string
	if parentHandoffID != "" {
		parent = &parentHandoffID
	}
	input := orchestrator.CreateHandoffInput{
		WorkflowKind:                  workflowKind,
		Sender:                        senderRef,
		Receiver:                      receiverRef,
		TaskKind:                      orchestrator.TaskKind(taskKind),
		Intent:                        intent,
		ParentHandoffID:               parent,
		DependsOnHandoffIDs:           splitCSV(dependsOn),
		RequiredForWorkflowCompletion: required,
		PayloadRef:                    payloadRef,
		DeliveryTargetRef:             deliveryTargetRef,
	}
	var result orchestrator.CreateHandoffResult
	if workflowID == "" {
		result, err = svc.CreateHandoff(context.Background(), input)
	} else {
		result, err = svc.AppendHandoff(context.Background(), orchestrator.AppendHandoffInput{WorkflowID: workflowID, Handoff: input})
	}
	if err != nil {
		return err
	}
	return printJSON(stdout, map[string]any{
		"workflow_id": result.Workflow.ID,
		"handoff_id":  result.Handoff.ID,
		"watches":     result.Watches,
	})
}

func runHandoffGet(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("handoff get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" {
		return fmt.Errorf("missing db or handoff-id")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	handoff, err := loadHandoff(context.Background(), store, handoffID)
	if err != nil {
		return err
	}
	return printJSON(stdout, handoff)
}

func runHandoffList(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("handoff list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" {
		return fmt.Errorf("missing db")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	handoffs, err := store.ListHandoffs(context.Background())
	if err != nil {
		return err
	}
	return printJSON(stdout, handoffs)
}

func runHandoffDispatch(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("handoff dispatch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	var adapter string
	var command string
	var target string
	var message string
	var adapterArgs string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	fs.StringVar(&adapter, "adapter", "", "adapter")
	fs.StringVar(&command, "command", "", "dispatch command")
	fs.StringVar(&target, "target", "", "target")
	fs.StringVar(&message, "message", "", "dispatch message")
	fs.StringVar(&adapterArgs, "args", "", "comma-separated adapter args")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" || adapter == "" || target == "" {
		return fmt.Errorf("missing db, handoff-id, adapter, or target")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	if adapter == "openclaw" && command != "" {
		svc.SetOpenClawAdapter(orchestrator.NewOpenClawAdapter(orchestrator.CommandRunner{}))
	}
	result, err := svc.DispatchHandoff(context.Background(), orchestrator.DispatchHandoffInput{
		HandoffID: handoffID,
		Adapter:   adapter,
		Target:    target,
		Command:   command,
		Args:      splitCSV(adapterArgs),
		Message:   message,
	})
	if err != nil {
		return err
	}
	return printJSON(stdout, result)
}

func runEventRecord(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("event record", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var dbPath string
	var workflowID string
	var handoffID string
	var eventType string
	var subjectActor string
	var producerActor string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&workflowID, "workflow-id", "", "workflow id")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	fs.StringVar(&eventType, "type", "", "event type")
	fs.StringVar(&subjectActor, "subject-actor", "", "subject actor")
	fs.StringVar(&producerActor, "producer-actor", "", "producer actor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if producerActor == "" {
		return fmt.Errorf("missing producer actor")
	}
	eventKind := orchestrator.EventType(eventType)
	if eventRequiresSubjectActor(eventKind) && subjectActor == "" {
		return fmt.Errorf("missing subject actor")
	}
	if dbPath == "" || workflowID == "" || handoffID == "" || eventType == "" {
		return fmt.Errorf("missing db, workflow-id, handoff-id, or type")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	var subjectRef orchestrator.ActorRef
	if subjectActor != "" {
		subjectRef, err = parseActorRef(subjectActor)
		if err != nil {
			return err
		}
	}
	producerRef, err := parseActorRef(producerActor)
	if err != nil {
		return err
	}
	decision, err := svc.RecordEvent(context.Background(), orchestrator.RecordEventInput{Event: orchestrator.EventRecord{
		WorkflowID:    workflowID,
		HandoffID:     handoffID,
		Type:          eventKind,
		SubjectActor:  subjectRef,
		ProducerActor: producerRef,
	}})
	if err != nil {
		return err
	}
	return printJSON(stdout, decision)
}

func runEventList(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("event list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" {
		return fmt.Errorf("missing db or handoff-id")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	events, err := store.ListEvents(context.Background(), handoffID)
	if err != nil {
		return err
	}
	return printJSON(stdout, events)
}

func runWorkflowStatus(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("workflow status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var workflowID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&workflowID, "workflow-id", "", "workflow id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || workflowID == "" {
		return fmt.Errorf("missing db or workflow-id")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	view, err := svc.WorkflowStatus(context.Background(), workflowID)
	if err != nil {
		return err
	}
	return printJSON(stdout, view)
}

func runWorkflowList(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("workflow list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" {
		return fmt.Errorf("missing db")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	workflows, err := store.ListWorkflows(context.Background())
	if err != nil {
		return err
	}
	views := make([]orchestrator.WorkflowView, 0, len(workflows))
	for _, workflow := range workflows {
		view, err := svc.WorkflowStatus(context.Background(), workflow.ID)
		if err != nil {
			return err
		}
		views = append(views, view)
	}
	return printJSON(stdout, views)
}

func runWatchList(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("watch list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" {
		return fmt.Errorf("missing db or handoff-id")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	watches, err := store.ListWatches(context.Background(), handoffID)
	if err != nil {
		return err
	}
	return printJSON(stdout, watches)
}

func runWatchRun(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("watch run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var nowRaw string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&nowRaw, "now", "", "current time")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || nowRaw == "" {
		return fmt.Errorf("missing db or now")
	}
	now, err := time.Parse(time.RFC3339Nano, nowRaw)
	if err != nil {
		return err
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	result, err := svc.RunWatchdog(context.Background(), orchestrator.RunWatchdogInput{Now: now})
	if err != nil {
		return err
	}
	return printJSON(stdout, result)
}

func runHandoffTimeline(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("handoff timeline", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" {
		return fmt.Errorf("missing db or handoff-id")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	events, err := store.ListEventIngestionAudit(context.Background(), handoffID)
	if err != nil {
		return err
	}
	return printJSON(stdout, events)
}

func runHandoffProtocolAction(args []string, stdout, stderr io.Writer, action orchestrator.ProtocolAction) error {
	_ = stderr
	fs := flag.NewFlagSet(string(action), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var workflowID string
	var handoffID string
	var actor string
	var artifactCount int
	var reviewDecision string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&workflowID, "workflow-id", "", "workflow id")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	fs.StringVar(&actor, "actor", "", "actor")
	fs.IntVar(&artifactCount, "artifact-count", 0, "artifact count")
	fs.StringVar(&reviewDecision, "review-decision", "", "review decision")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" || actor == "" {
		return fmt.Errorf("missing db, handoff-id, or actor")
	}
	if artifactCount < 0 {
		return fmt.Errorf("artifact-count must be >= 0")
	}
	switch action {
	case orchestrator.ProtocolActionReview:
		if !isValidReviewDecision(orchestrator.ReviewDecision(reviewDecision)) {
			return fmt.Errorf("handoff review requires valid review decision")
		}
	case orchestrator.ProtocolActionApprove, orchestrator.ProtocolActionRequestRevision:
		if reviewDecision != "" {
			return fmt.Errorf("%s does not accept review-decision", action)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	actorRef, err := parseActorRef(actor)
	if err != nil {
		return err
	}
	result, err := svc.ApplyProtocolAction(context.Background(), orchestrator.ProtocolRequest{
		Action:         action,
		WorkflowID:     workflowID,
		HandoffID:      handoffID,
		Actor:          actorRef,
		ArtifactCount:  artifactCount,
		ReviewDecision: orchestrator.ReviewDecision(reviewDecision),
	})
	if err != nil {
		return err
	}
	return printJSON(stdout, result)
}

func runSignalRecord(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("signal record", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var workflowID string
	var handoffID string
	var signalType string
	var producerActor string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&workflowID, "workflow-id", "", "workflow id")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	fs.StringVar(&signalType, "type", "", "signal type")
	fs.StringVar(&producerActor, "producer-actor", "", "producer actor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || workflowID == "" || handoffID == "" || signalType == "" || producerActor == "" {
		return fmt.Errorf("missing db, workflow-id, handoff-id, type, or producer-actor")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	handoff, err := store.LoadHandoff(context.Background(), handoffID)
	if err != nil {
		return err
	}
	if workflowID != handoff.WorkflowID {
		return fmt.Errorf("workflow-id does not match handoff")
	}
	svc := orchestrator.NewService(store, nil)
	producerRef, err := parseActorRef(producerActor)
	if err != nil {
		return err
	}
	if producerRef.Type != orchestrator.ActorSystem {
		return fmt.Errorf("signal record requires system producer actor")
	}
	event := orchestrator.EventRecord{
		WorkflowID:    workflowID,
		HandoffID:     handoffID,
		Type:          orchestrator.EventType(signalType),
		ProducerActor: producerRef,
	}
	if err := svc.RecordObservedSignal(context.Background(), orchestrator.RecordObserverHintInput{Event: event}); err != nil {
		return err
	}
	return printJSON(stdout, map[string]any{"recorded": true, "type": signalType, "handoff_id": handoffID})
}

func runSignalList(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("signal list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" {
		return fmt.Errorf("missing db or handoff-id")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	signals, err := store.ListObservedSignalsByHandoff(context.Background(), handoffID)
	if err != nil {
		return err
	}
	return printJSON(stdout, signals)
}

func runDivergenceList(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("divergence list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" {
		return fmt.Errorf("missing db or handoff-id")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	divergences, err := store.ListDivergences(context.Background(), handoffID)
	if err != nil {
		return err
	}
	return printJSON(stdout, divergences)
}

func runRepairInvalidateEvent(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("repair invalidate-event", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var eventID string
	var reason string
	var actor string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&eventID, "event-id", "", "event id")
	fs.StringVar(&reason, "reason", "", "reason")
	fs.StringVar(&actor, "actor", "", "actor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || eventID == "" || reason == "" || actor == "" {
		return fmt.Errorf("missing db, event-id, reason, or actor")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	actorRef, err := parseActorRef(actor)
	if err != nil {
		return err
	}
	repair, err := svc.InvalidateEvent(context.Background(), orchestrator.InvalidateEventInput{
		EventID: eventID,
		Reason:  reason,
		Actor:   actorRef,
	})
	if err != nil {
		return err
	}
	return printJSON(stdout, repair)
}

func runRepairReopenHandoff(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("repair reopen-handoff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	var reason string
	var actor string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	fs.StringVar(&reason, "reason", "", "reason")
	fs.StringVar(&actor, "actor", "", "actor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" || reason == "" || actor == "" {
		return fmt.Errorf("missing db, handoff-id, reason, or actor")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	actorRef, err := parseActorRef(actor)
	if err != nil {
		return err
	}
	repair, err := svc.ReopenHandoff(context.Background(), handoffID, reason, actorRef)
	if err != nil {
		return err
	}
	return printJSON(stdout, repair)
}

func runRepairBackfillEvent(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("repair backfill-event", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var workflowID string
	var handoffID string
	var eventType string
	var subjectActor string
	var producerActor string
	var requestedBy string
	var reason string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&workflowID, "workflow-id", "", "workflow id")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	fs.StringVar(&eventType, "type", "", "event type")
	fs.StringVar(&subjectActor, "subject-actor", "", "subject actor")
	fs.StringVar(&producerActor, "producer-actor", "", "producer actor")
	fs.StringVar(&requestedBy, "requested-by", "", "requested by actor")
	fs.StringVar(&reason, "reason", "", "reason")
	if err := fs.Parse(args); err != nil {
		return err
	}
	eventKind := orchestrator.EventType(eventType)
	if dbPath == "" || workflowID == "" || handoffID == "" || eventType == "" || producerActor == "" || requestedBy == "" || reason == "" {
		return fmt.Errorf("missing required backfill-event flags")
	}
	if eventRequiresSubjectActor(eventKind) && subjectActor == "" {
		return fmt.Errorf("missing required backfill-event flags")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	svc := orchestrator.NewService(store, nil)
	var subjectRef orchestrator.ActorRef
	if subjectActor != "" {
		subjectRef, err = parseActorRef(subjectActor)
		if err != nil {
			return err
		}
	}
	producerRef, err := parseActorRef(producerActor)
	if err != nil {
		return err
	}
	requestedByRef, err := parseActorRef(requestedBy)
	if err != nil {
		return err
	}
	repair, err := svc.BackfillEvent(context.Background(), orchestrator.BackfillEventInput{
		Event: orchestrator.EventRecord{
			WorkflowID:    workflowID,
			HandoffID:     handoffID,
			Type:          eventKind,
			SubjectActor:  subjectRef,
			ProducerActor: producerRef,
		},
		Reason:      reason,
		RequestedBy: requestedByRef,
	})
	if err != nil {
		return err
	}
	return printJSON(stdout, repair)
}

func runRepairList(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("repair list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" {
		return fmt.Errorf("missing db")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	repairs, err := store.ListRepairs(context.Background(), handoffID)
	if err != nil {
		return err
	}
	return printJSON(stdout, repairs)
}

func runRepairCandidateList(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("repair-candidate list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" {
		return fmt.Errorf("missing db or handoff-id")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	candidates, err := store.ListRepairCandidatesByHandoff(context.Background(), handoffID)
	if err != nil {
		return err
	}
	return printJSON(stdout, candidates)
}

func runOwnershipGet(args []string, stdout, stderr io.Writer) error {
	_ = stderr
	fs := flag.NewFlagSet("ownership get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var dbPath string
	var handoffID string
	fs.StringVar(&dbPath, "db", "", "sqlite db path")
	fs.StringVar(&handoffID, "handoff-id", "", "handoff id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dbPath == "" || handoffID == "" {
		return fmt.Errorf("missing db or handoff-id")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := orchestrator.NewStore(context.Background(), db)
	if err != nil {
		return err
	}
	binding, err := store.LoadOwnershipBinding(context.Background(), handoffID)
	if err != nil {
		return err
	}
	return printJSON(stdout, binding)
}

func eventRequiresSubjectActor(eventType orchestrator.EventType) bool {
	switch eventType {
	case orchestrator.EventTransportRequested,
		orchestrator.EventTransportAccepted,
		orchestrator.EventTransportRejected,
		orchestrator.EventTransportTimeout,
		orchestrator.EventTransportDeliveryConfirmed,
		orchestrator.EventExpired:
		return false
	default:
		return true
	}
}

func parseActorRef(raw string) (orchestrator.ActorRef, error) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return orchestrator.ActorRef{}, fmt.Errorf("invalid actor %q", raw)
	}
	return orchestrator.ActorRef{Type: orchestrator.ActorType(parts[0]), ID: parts[1]}, nil
}

func isValidReviewDecision(decision orchestrator.ReviewDecision) bool {
	switch decision {
	case orchestrator.ReviewDecisionApproved,
		orchestrator.ReviewDecisionRevisionRequired,
		orchestrator.ReviewDecisionRejected:
		return true
	default:
		return false
	}
}

func splitCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func loadHandoff(ctx context.Context, store *orchestrator.Store, handoffID string) (orchestrator.Handoff, error) {
	return store.LoadHandoff(ctx, handoffID)
}
