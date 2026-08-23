// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The service phase graph and its shutdown budgets, pinned.
//!
//! Both are cross-language contracts. `libs/go/servicekit/lifecycle_contract_test.go`
//! pins the same matrix, the same phase names, and the same probe answers
//! against the Go runtime; a change made on one side and not the other fails
//! one of the two suites.

use mindclade_faults::{Code, FaultResult};
use mindclade_runtime_core::{Clock, ManualClock};
use mindclade_servicekit::{Component, Lifecycle, LifecycleState, Service, ServiceConfig};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant, SystemTime};

const ALL: [LifecycleState; 7] = [
    LifecycleState::Created,
    LifecycleState::Starting,
    LifecycleState::Running,
    LifecycleState::Draining,
    LifecycleState::Stopping,
    LifecycleState::Stopped,
    LifecycleState::Failed,
];

/// Every edge the phase graph admits, and nothing else.
fn legal(from: LifecycleState, to: LifecycleState) -> bool {
    use LifecycleState::{Created, Draining, Failed, Running, Starting, Stopped, Stopping};
    matches!(
        (from, to),
        (Created, Starting | Failed)
            | (Starting, Running | Draining | Stopping | Failed)
            | (Running, Draining | Stopping | Failed)
            | (Draining, Stopping | Failed)
            | (Stopping, Stopped | Failed)
    )
}

#[test]
fn transition_matrix_is_exhaustively_pinned() {
    for from in ALL {
        for to in ALL {
            let lifecycle = Lifecycle::new();
            // Walk to `from` along a path the graph already admits, so the
            // assertion below is about the edge under test and nothing else.
            for step in path_to(from) {
                lifecycle
                    .transition(step)
                    .unwrap_or_else(|fault| panic!("reaching {from:?} via {step:?}: {fault:?}"));
            }
            let accepted = lifecycle.transition(to).is_ok();
            assert_eq!(
                accepted,
                legal(from, to),
                "{from:?} -> {to:?} acceptance disagrees with the pinned phase graph",
            );
            assert_eq!(
                from.can_transition_to(to),
                legal(from, to),
                "{from:?}.can_transition_to({to:?}) disagrees with the pinned phase graph",
            );
        }
        assert_eq!(
            from.is_terminal(),
            ALL.iter().all(|to| !legal(from, *to)),
            "{from:?} terminality disagrees with its outgoing edges",
        );
    }
}

/// Phase names cross a wire. An operator correlating a Rust node's drain with
/// the control plane's view of it is matching these strings, so they are the
/// Go runtime's `service_state` vocabulary verbatim.
#[test]
fn phase_names_match_the_go_runtime() {
    let expected = [
        (LifecycleState::Created, "new"),
        (LifecycleState::Starting, "starting"),
        (LifecycleState::Running, "running"),
        (LifecycleState::Draining, "draining"),
        (LifecycleState::Stopping, "stopping"),
        (LifecycleState::Stopped, "stopped"),
        (LifecycleState::Failed, "failed"),
    ];
    for (state, name) in expected {
        assert_eq!(
            state.as_str(),
            name,
            "{state:?} reports the wrong phase name"
        );
        assert_eq!(state.to_string(), name, "{state:?} displays the wrong name");
    }
}

/// Probe answers are a function of the phase, and orchestration routes on them.
/// This table is `TestProbeSemanticsPerPhase` in the Go suite.
#[test]
fn probe_predicates_match_the_go_runtime() {
    let expected = [
        (LifecycleState::Created, false, false),
        (LifecycleState::Starting, true, false),
        (LifecycleState::Running, true, true),
        (LifecycleState::Draining, true, false),
        (LifecycleState::Stopping, true, false),
        (LifecycleState::Stopped, false, false),
        (LifecycleState::Failed, false, false),
    ];
    for (state, live, ready) in expected {
        assert_eq!(state.is_live(), live, "{state:?} liveness");
        assert_eq!(state.admits_traffic(), ready, "{state:?} readiness");
    }
}

/// A drain that outlives its budget is not a slow drain. The grace period ends,
/// the process is killed mid-request, and the drain achieved nothing. The pass
/// therefore stops calling hooks once the budget is spent and says so.
#[test]
fn drain_pass_is_bounded_by_its_budget() {
    let clock = Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now()));
    let log = Arc::new(Mutex::new(Vec::new()));
    let mut service = service_with_budgets(&clock, Duration::from_secs(1), Duration::from_secs(2));
    service
        .register(costly("database", &clock, &log, Duration::ZERO))
        .expect("registers");
    service
        .register(costly("server", &clock, &log, Duration::from_secs(5)))
        .expect("registers");
    service.start().expect("starts");

    let fault = service
        .drain()
        .expect_err("an overspent drain must be reported");
    assert_eq!(fault.code(), Code::DeadlineExceeded);
    // Reverse order: `server` drains first and spends the whole budget, so
    // `database` never gets its hook and the pass says which one was skipped.
    assert_eq!(events(&log), ["server:drain"]);
    assert_eq!(service.lifecycle().state(), LifecycleState::Draining);
}

/// An overspent drain must not consume the stop pass.
///
/// The two budgets are separate for this reason: a drain hook that runs past
/// `drain_timeout` still leaves the rest of `shutdown_timeout` for stopping, so
/// listeners are closed and handles released rather than left to the SIGKILL.
#[test]
fn an_overspent_drain_still_leaves_the_stop_pass_its_budget() {
    let clock = Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now()));
    let log = Arc::new(Mutex::new(Vec::new()));
    let mut service = service_with_budgets(&clock, Duration::from_secs(1), Duration::from_secs(10));
    service
        .register(costly("database", &clock, &log, Duration::ZERO))
        .expect("registers");
    service
        .register(costly("server", &clock, &log, Duration::from_secs(2)))
        .expect("registers");
    service.start().expect("starts");

    let fault = service
        .stop()
        .expect_err("an overspent drain must be reported");
    assert_eq!(fault.code(), Code::DeadlineExceeded);
    // `server` spent the drain budget, so `database` lost its drain hook — and
    // both still received their stop hook.
    assert_eq!(
        events(&log),
        ["server:drain", "server:stop", "database:stop"]
    );
    assert!(service.lifecycle().state().is_terminal());
}

/// When the whole shutdown budget is gone the pass ends, and the service still
/// reaches a terminal phase rather than sitting in `stopping` forever.
///
/// Ending there is deliberate and matches the Go coordinator, whose stop pass
/// stops calling hooks once its shutdown context expires: past that point the
/// orchestrator is already removing the process, and the remaining hooks would
/// run into a SIGKILL. A synchronous hook that overruns cannot be cancelled
/// from here, so this is the one path that can still skip a stop hook.
#[test]
fn an_exhausted_shutdown_budget_ends_the_pass() {
    let clock = Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now()));
    let log = Arc::new(Mutex::new(Vec::new()));
    let mut service = service_with_budgets(&clock, Duration::from_secs(1), Duration::from_secs(2));
    service
        .register(costly("database", &clock, &log, Duration::ZERO))
        .expect("registers");
    service
        .register(costly("server", &clock, &log, Duration::from_secs(5)))
        .expect("registers");
    service.start().expect("starts");

    let fault = service
        .stop()
        .expect_err("an overspent shutdown must be reported");
    assert_eq!(fault.code(), Code::DeadlineExceeded);
    assert_eq!(events(&log), ["server:drain"]);
    assert!(
        service.lifecycle().state().is_terminal(),
        "an overspent shutdown must still reach a terminal phase, got {}",
        service.lifecycle().state(),
    );
}

/// A shutdown inside its budget is untouched by the bound: every started
/// component drains and stops, in reverse registration order.
#[test]
fn shutdown_within_budget_visits_every_component() {
    let clock = Arc::new(ManualClock::new(SystemTime::UNIX_EPOCH, Instant::now()));
    let log = Arc::new(Mutex::new(Vec::new()));
    let mut service = service_with_budgets(&clock, Duration::from_secs(1), Duration::from_secs(2));
    service
        .register(costly("database", &clock, &log, Duration::from_millis(10)))
        .expect("registers");
    service
        .register(costly("server", &clock, &log, Duration::from_millis(10)))
        .expect("registers");
    service.start().expect("starts");

    service
        .stop()
        .expect("a shutdown inside its budget succeeds");
    assert_eq!(
        events(&log),
        [
            "server:drain",
            "database:drain",
            "server:stop",
            "database:stop"
        ]
    );
    assert_eq!(service.lifecycle().state(), LifecycleState::Stopped);
}

/// A drain budget that leaves the stop pass nothing is a configuration error,
/// not a longer drain. Equal budgets are rejected too: a drain permitted to
/// spend the whole shutdown budget can leave every stop hook uncalled.
#[test]
fn drain_budget_must_leave_the_stop_pass_a_budget() {
    for (drain, shutdown) in [(30, 10), (10, 10)] {
        let config = ServiceConfig {
            name: String::from("gateway"),
            drain_timeout: Duration::from_secs(drain),
            shutdown_timeout: Duration::from_secs(shutdown),
        };
        let outcome = config.validate();
        assert!(
            outcome.is_err(),
            "drain {drain}s with shutdown {shutdown}s must be rejected",
        );
        assert_eq!(outcome.unwrap_err().code(), Code::InvalidArgument);
    }
}

/// The shortest admitted path to each state, used to set up the matrix test.
fn path_to(state: LifecycleState) -> Vec<LifecycleState> {
    use LifecycleState::{Created, Draining, Failed, Running, Starting, Stopped, Stopping};
    match state {
        Created => vec![],
        Starting => vec![Starting],
        Running => vec![Starting, Running],
        Draining => vec![Starting, Running, Draining],
        Stopping => vec![Starting, Running, Draining, Stopping],
        Stopped => vec![Starting, Running, Draining, Stopping, Stopped],
        Failed => vec![Failed],
    }
}

fn service_with_budgets(clock: &Arc<ManualClock>, drain: Duration, shutdown: Duration) -> Service {
    let clock: Arc<dyn Clock> = clock.clone();
    Service::with_config(
        ServiceConfig {
            name: String::from("gateway"),
            drain_timeout: drain,
            shutdown_timeout: shutdown,
        },
        clock,
    )
    .expect("valid configuration")
}

fn events(log: &Arc<Mutex<Vec<String>>>) -> Vec<String> {
    log.lock()
        .unwrap_or_else(std::sync::PoisonError::into_inner)
        .clone()
}

/// A component whose hooks consume a known amount of the shutdown budget.
struct CostlyComponent {
    name: &'static str,
    clock: Arc<ManualClock>,
    log: Arc<Mutex<Vec<String>>>,
    cost: Duration,
}

impl CostlyComponent {
    fn record(&self, action: &str) {
        self.log
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner)
            .push(format!("{}:{action}", self.name));
        self.clock.advance(self.cost);
    }
}

impl Component for CostlyComponent {
    fn name(&self) -> &str {
        self.name
    }
    fn start(&mut self) -> FaultResult<()> {
        Ok(())
    }
    fn drain(&mut self) -> FaultResult<()> {
        self.record("drain");
        Ok(())
    }
    fn stop(&mut self) -> FaultResult<()> {
        self.record("stop");
        Ok(())
    }
}

fn costly(
    name: &'static str,
    clock: &Arc<ManualClock>,
    log: &Arc<Mutex<Vec<String>>>,
    cost: Duration,
) -> Box<dyn Component> {
    Box::new(CostlyComponent {
        name,
        clock: clock.clone(),
        log: log.clone(),
        cost,
    })
}
