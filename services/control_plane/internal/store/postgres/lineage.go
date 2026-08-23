// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"go.mindclade.dev/control/lineage"
	"go.mindclade.dev/libs/go/faults"
	"go.mindclade.dev/libs/go/identifiers"
)

var _ lineage.Repository = lineageGraphs{}

// lineageGraphs adapts Store to lineage.Repository.
//
// It is a separate type rather than two more methods on Store because Store
// already carries Put and Get for artifacts.Catalog with different signatures,
// and one Go type cannot spell the same method name twice. The alternative --
// renaming the catalog's methods -- would change a contract that already has a
// caller to accommodate one that does not.
//
// It holds the Store rather than its own *sql.DB so it resolves the same
// executor: a lineage write joins the caller's transaction, and a publication
// that must commit with a release promotion and its audit record reaches one
// commit rather than three.
type lineageGraphs struct{ store *Store }

// LineageGraphs returns the durable lineage.Repository backed by this Store.
func (store *Store) LineageGraphs() lineage.Repository { return lineageGraphs{store: store} }

// Put binds a graph to its digest permanently.
//
// The binding is what makes the record useful: a lineage graph is quoted by
// digest, so a digest that could be repointed at different provenance would let
// a release cite evidence it was never built from. Re-publishing identical
// provenance must still succeed, because a lost response is the ordinary case
// and the caller's only recovery is to send the same graph again.
func (repository lineageGraphs) Put(ctx context.Context, digest identifiers.Digest, graph lineage.Graph) error {
	const operation = "registry.postgres.PutLineageGraph"
	store := repository.store
	if err := store.validate(ctx, operation); err != nil {
		return err
	}
	// Digest validates the graph on the way through, so this one call rejects
	// an unbounded, cyclic, subject-less, or duplicated graph as well. The
	// domain's node and edge maxima are therefore enforced before any row is
	// written; the table's CHECK constraints below are the second copy.
	computed, err := graph.Digest()
	if err != nil {
		return invalidLineage(ctx, err, operation, faults.ReasonOf(err))
	}
	// A caller that files a graph under a digest that is not its own is
	// attempting exactly the rebinding this store exists to refuse, and it must
	// be refused before the insert rather than discovered on the next read.
	if !computed.Equal(digest) {
		return invalidLineage(ctx, lineage.ErrGraphImmutable, operation, "lineage_digest_binding_mismatch")
	}
	document, err := json.Marshal(graph)
	if err != nil {
		return internal(ctx, err, operation, "lineage_encoding_failed")
	}

	query := fmt.Sprintf(`INSERT INTO %s (
graph_digest, graph_id, subject_digest, policy_digest, schema_version, node_count, edge_count, document, written_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (graph_digest) DO NOTHING`, store.lineage)
	result, err := store.executor(ctx).ExecContext(ctx, query,
		digest.String(), graph.GraphID, graph.SubjectDigest.String(), graph.PolicyDigest.String(),
		int64(graph.SchemaVersion), int64(len(graph.Nodes)), int64(len(graph.Edges)),
		document, store.clock.Now().Round(0).UTC(),
	)
	if err != nil {
		return provider(ctx, err, operation)
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected == 1 {
		return nil
	}
	return repository.reconcileStoredGraph(ctx, digest, document, operation)
}

// reconcileStoredGraph decides whether a conflicting insert was a replay or a
// rebinding. Zero rows affected only says a row already exists under this
// digest; it does not say whether that row holds the same provenance.
func (repository lineageGraphs) reconcileStoredGraph(ctx context.Context, digest identifiers.Digest, document []byte, operation string) error {
	store := repository.store
	var stored []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE graph_digest=$1`, store.lineage), digest.String(),
	).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return internal(ctx, lineage.ErrGraphNotFound, operation, "lineage_graph_write_lost")
	}
	if err != nil {
		return provider(ctx, err, operation)
	}
	if bytes.Equal(stored, document) {
		return nil
	}
	// Byte inequality is not yet a rebinding. The graph's canonical digest is
	// order-independent and covers every field the document carries, so a row
	// written by a different encoder -- a reordered node slice, a marshaller
	// that spaces JSON differently -- can hold the same provenance under
	// different bytes. Recomputing is what distinguishes a benign re-encoding
	// from a digest bound to a different graph.
	var existing lineage.Graph
	if err := json.Unmarshal(stored, &existing); err != nil {
		return internal(ctx, err, operation, "lineage_decoding_failed")
	}
	existingDigest, err := existing.Digest()
	if err != nil {
		return internal(ctx, err, operation, "stored_lineage_graph_invalid")
	}
	if existingDigest.Equal(digest) {
		return nil
	}
	return immutableLineage(ctx, operation, digest, existingDigest)
}

// Get returns the graph stored under digest, exactly as it was stored.
//
// It deliberately does not recompute the digest and reject a mismatch.
// lineage.Service.Get is the corruption detector: it recomputes on every read
// and raises CodeDataLoss with reason lineage_digest_mismatch, which is the
// signal an operator is paged on. A check here would consume the mismatch first
// and report it under a different code, leaving the service's detector
// unreachable and the documented failure mode unobservable. The same reason
// the graph is not re-validated here: an unvalidatable stored graph fails
// Digest inside the service, which is where the caller expects to learn it.
//
// A row that will not decode at all is different -- there is no graph to hand
// back, corrupt or otherwise -- so that is reported here.
func (repository lineageGraphs) Get(ctx context.Context, digest identifiers.Digest) (lineage.Graph, error) {
	const operation = "registry.postgres.GetLineageGraph"
	store := repository.store
	if err := store.validate(ctx, operation); err != nil {
		return lineage.Graph{}, err
	}
	if !digest.Valid() {
		return lineage.Graph{}, invalidLineage(ctx, lineage.ErrGraphNotFound, operation, "lineage_digest_invalid")
	}
	var document []byte
	err := store.executor(ctx).QueryRowContext(ctx,
		fmt.Sprintf(`SELECT document FROM %s WHERE graph_digest=$1`, store.lineage), digest.String(),
	).Scan(&document)
	if errors.Is(err, sql.ErrNoRows) {
		return lineage.Graph{}, lineageMissing(ctx, operation, digest)
	}
	if err != nil {
		return lineage.Graph{}, provider(ctx, err, operation)
	}
	var graph lineage.Graph
	if err := json.Unmarshal(document, &graph); err != nil {
		return lineage.Graph{}, internal(ctx, err, operation, "lineage_decoding_failed")
	}
	return graph, nil
}

func invalidLineage(ctx context.Context, err error, operation, reason string) error {
	return faults.Wrap(err, faults.CodeInvalidArgument, "lineage graph is invalid",
		faults.WithReason(reason), faults.WithOperation(operation),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

func lineageMissing(ctx context.Context, operation string, digest identifiers.Digest) error {
	return faults.Wrap(lineage.ErrGraphNotFound, faults.CodeNotFound, "lineage graph was not found",
		faults.WithReason(lineage.ReasonGraphNotFound), faults.WithOperation(operation),
		faults.WithField("graph_digest", digest.String()),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}

// immutableLineage carries both digests because the operator question after
// this fault is "what is already stored there", and a message naming only the
// requested digest cannot answer it.
func immutableLineage(ctx context.Context, operation string, requested, stored identifiers.Digest) error {
	return faults.Wrap(lineage.ErrGraphImmutable, faults.CodeFailedPrecondition,
		"lineage graph digest is already bound to a different graph",
		faults.WithReason(lineage.ReasonGraphImmutable), faults.WithOperation(operation),
		faults.WithField("graph_digest", requested.String()),
		faults.WithField("stored_graph_digest", stored.String()),
		faults.WithContextMetadata(ctx), faults.WithRetryPolicy(faults.NoRetry()))
}
