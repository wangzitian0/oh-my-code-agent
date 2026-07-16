package domain

import "testing"

// These tests exercise the required-field and closed-enum branches of the
// six protocol document validators directly, the same way validation_test.go
// does for the desired-state documents, so every rejection branch has at
// least one caller and not just the golden/invalid fixtures.

func baseObservation() Observation {
	return Observation{
		APIVersion: SupportedAPIVersion,
		Kind:       "Observation",
		Metadata:   Metadata{ID: "obs:1"},
		Spec: ObservationSpec{
			Host:          ObservationHost{ID: "codex"},
			Concept:       "instruction",
			Source:        ObservationSource{Kind: "file"},
			Scope:         ObservationScope{Kind: "workspace"},
			Disposition:   DispositionActive,
			EvidenceLevel: EvidenceLevelResolved,
		},
	}
}

func TestValidateObservation_RequiredFields(t *testing.T) {
	base := baseObservation()
	if err := ValidateObservation(base); err != nil {
		t.Fatalf("baseline observation should validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Observation)
	}{
		{"bad apiVersion", func(o *Observation) { o.APIVersion = "omca.dev/v2" }},
		{"bad kind", func(o *Observation) { o.Kind = "Something" }},
		{"missing metadata.id", func(o *Observation) { o.Metadata = Metadata{} }},
		{"missing host.id", func(o *Observation) { o.Spec.Host.ID = "" }},
		{"unknown host.id", func(o *Observation) { o.Spec.Host.ID = "not-a-host" }},
		{"missing concept", func(o *Observation) { o.Spec.Concept = "" }},
		{"missing source.kind", func(o *Observation) { o.Spec.Source.Kind = "" }},
		{"missing scope.kind", func(o *Observation) { o.Spec.Scope.Kind = "" }},
		{"unknown scope.kind", func(o *Observation) { o.Spec.Scope.Kind = "galaxy" }},
		{"invalid disposition", func(o *Observation) { o.Spec.Disposition = "HIDDEN" }},
		{"invalid evidenceLevel", func(o *Observation) { o.Spec.EvidenceLevel = "E9" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := base
			c.mutate(&o)
			if err := ValidateObservation(o); err == nil {
				t.Errorf("expected an error for %s", c.name)
			}
		})
	}
}

func baseHostKnowledge() HostKnowledge {
	return HostKnowledge{
		APIVersion: SupportedAPIVersion,
		Kind:       "HostKnowledge",
		Metadata: HostKnowledgeMetadata{
			ID: "codex:cli:0.144", Host: "codex", Surface: "cli",
			VersionRange: ">=0.144.0", Status: KnowledgeFresh,
		},
		Evidence: []KnowledgeEvidenceRef{{ID: "ev1", Kind: "official-doc"}},
		Capabilities: map[string]CapabilityOps{
			"skill": {Discover: CapabilityExact},
		},
	}
}

func TestValidateHostKnowledge_RequiredFields(t *testing.T) {
	base := baseHostKnowledge()
	if err := ValidateHostKnowledge(base); err != nil {
		t.Fatalf("baseline HostKnowledge should validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*HostKnowledge)
	}{
		{"bad apiVersion", func(h *HostKnowledge) { h.APIVersion = "x" }},
		{"bad kind", func(h *HostKnowledge) { h.Kind = "Something" }},
		{"missing metadata.id", func(h *HostKnowledge) { h.Metadata.ID = "" }},
		{"missing metadata.host", func(h *HostKnowledge) { h.Metadata.Host = "" }},
		{"unknown metadata.host", func(h *HostKnowledge) { h.Metadata.Host = "not-a-host" }},
		{"missing metadata.surface", func(h *HostKnowledge) { h.Metadata.Surface = "" }},
		{"missing metadata.versionRange", func(h *HostKnowledge) { h.Metadata.VersionRange = "" }},
		{"invalid status", func(h *HostKnowledge) { h.Metadata.Status = "ARCHIVED" }},
		{"empty evidence", func(h *HostKnowledge) { h.Evidence = nil }},
		{"evidence missing id", func(h *HostKnowledge) { h.Evidence = []KnowledgeEvidenceRef{{Kind: "official-doc"}} }},
		{"invalid capability level", func(h *HostKnowledge) {
			h.Capabilities = map[string]CapabilityOps{"skill": {Discover: "VENDOR_ONLY"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := base
			c.mutate(&h)
			if err := ValidateHostKnowledge(h); err == nil {
				t.Errorf("expected an error for %s", c.name)
			}
		})
	}
}

func baseReport() Report {
	return Report{
		APIVersion: SupportedAPIVersion,
		Kind:       "Report",
		Metadata:   ReportMetadata{ID: "report:1", Worktree: "worktree:1", GeneratedAt: "2026-07-16T00:00:00Z"},
		Spec: ReportSpec{
			Fingerprint: "sha256:88ab5e32bbf5205cac37d9464c219373ac4bbbb169bcfc5035bf5baf56919202",
			Drift: []DriftAssertion{
				{EntityID: "e1", Field: "f1", Category: DriftUnknown, RootCause: "rc"},
			},
			KnowledgeStatus: map[string]KnowledgeStatus{"codex": KnowledgeFresh},
		},
	}
}

func TestValidateReport_RequiredFields(t *testing.T) {
	base := baseReport()
	if err := ValidateReport(base); err != nil {
		t.Fatalf("baseline Report should validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Report)
	}{
		{"bad apiVersion", func(r *Report) { r.APIVersion = "x" }},
		{"bad kind", func(r *Report) { r.Kind = "Something" }},
		{"missing metadata.id", func(r *Report) { r.Metadata.ID = "" }},
		{"missing metadata.worktree", func(r *Report) { r.Metadata.Worktree = "" }},
		{"missing metadata.generatedAt", func(r *Report) { r.Metadata.GeneratedAt = "" }},
		{"missing fingerprint", func(r *Report) { r.Spec.Fingerprint = "" }},
		{"malformed fingerprint", func(r *Report) { r.Spec.Fingerprint = "not-a-digest" }},
		{"drift missing entityId", func(r *Report) { r.Spec.Drift[0].EntityID = "" }},
		{"drift missing field", func(r *Report) { r.Spec.Drift[0].Field = "" }},
		{"drift missing rootCause", func(r *Report) { r.Spec.Drift[0].RootCause = "" }},
		{"drift invalid category", func(r *Report) { r.Spec.Drift[0].Category = "MYSTERY" }},
		{"drift invalid evidenceLevel", func(r *Report) { r.Spec.Drift[0].EvidenceLevel = "E9" }},
		{"drift invalid guarantee", func(r *Report) { r.Spec.Drift[0].Guarantee = "MAYBE" }},
		{"invalid knowledgeStatus", func(r *Report) { r.Spec.KnowledgeStatus["codex"] = "ARCHIVED" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := base
			// Deep-copy the slice/map so mutations don't leak across cases.
			r.Spec.Drift = append([]DriftAssertion{}, base.Spec.Drift...)
			r.Spec.KnowledgeStatus = map[string]KnowledgeStatus{"codex": KnowledgeFresh}
			c.mutate(&r)
			if err := ValidateReport(r); err == nil {
				t.Errorf("expected an error for %s", c.name)
			}
		})
	}
}

func baseGeneration() Generation {
	return Generation{
		APIVersion: SupportedAPIVersion,
		Kind:       "Generation",
		Metadata:   GenerationMetadata{ID: "gen:1", Worktree: "worktree:1", CreatedAt: "2026-07-16T00:00:00Z"},
		Spec: GenerationSpec{
			DesiredGraphDigest: "sha256:88ab5e32bbf5205cac37d9464c219373ac4bbbb169bcfc5035bf5baf56919202",
			KnowledgePacks: []KnowledgePackRef{
				{ID: "codex:cli:0.144", Digest: "sha256:88ab5e32bbf5205cac37d9464c219373ac4bbbb169bcfc5035bf5baf56919202"},
			},
			Hosts: map[string]GenerationHostEntry{
				"codex": {AdapterID: "adapter:codex", Ownership: OwnershipManaged},
			},
		},
	}
}

func TestValidateGeneration_RequiredFields(t *testing.T) {
	base := baseGeneration()
	if err := ValidateGeneration(base); err != nil {
		t.Fatalf("baseline Generation should validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Generation)
	}{
		{"bad apiVersion", func(g *Generation) { g.APIVersion = "x" }},
		{"bad kind", func(g *Generation) { g.Kind = "Something" }},
		{"missing metadata.id", func(g *Generation) { g.Metadata.ID = "" }},
		{"missing metadata.worktree", func(g *Generation) { g.Metadata.Worktree = "" }},
		{"missing metadata.createdAt", func(g *Generation) { g.Metadata.CreatedAt = "" }},
		{"missing desiredGraphDigest", func(g *Generation) { g.Spec.DesiredGraphDigest = "" }},
		{"malformed desiredGraphDigest", func(g *Generation) { g.Spec.DesiredGraphDigest = "not-a-digest" }},
		{"knowledgePack missing id", func(g *Generation) {
			g.Spec.KnowledgePacks = []KnowledgePackRef{{Digest: g.Spec.DesiredGraphDigest}}
		}},
		{"knowledgePack malformed digest", func(g *Generation) {
			g.Spec.KnowledgePacks = []KnowledgePackRef{{ID: "x", Digest: "bad"}}
		}},
		{"unknown host key", func(g *Generation) {
			g.Spec.Hosts = map[string]GenerationHostEntry{"not-a-host": {AdapterID: "a", Ownership: OwnershipManaged}}
		}},
		{"host missing adapterId", func(g *Generation) {
			g.Spec.Hosts = map[string]GenerationHostEntry{"codex": {Ownership: OwnershipManaged}}
		}},
		{"host invalid ownership", func(g *Generation) {
			g.Spec.Hosts = map[string]GenerationHostEntry{"codex": {AdapterID: "a", Ownership: "OWNED_BY_NOBODY"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := base
			g.Spec.KnowledgePacks = append([]KnowledgePackRef{}, base.Spec.KnowledgePacks...)
			g.Spec.Hosts = map[string]GenerationHostEntry{"codex": base.Spec.Hosts["codex"]}
			c.mutate(&g)
			if err := ValidateGeneration(g); err == nil {
				t.Errorf("expected an error for %s", c.name)
			}
		})
	}
}

func baseRepairProposal() RepairProposal {
	return RepairProposal{
		APIVersion: SupportedAPIVersion,
		Kind:       "RepairProposal",
		Metadata:   Metadata{ID: "repair:1"},
		Spec: RepairProposalSpec{
			ReportFingerprint: "sha256:88ab5e32bbf5205cac37d9464c219373ac4bbbb169bcfc5035bf5baf56919202",
			Author:            RepairAuthor{Kind: "human"},
			Ownership:         OwnershipManaged,
			Changes:           []RepairChange{{TargetKind: "Profile", TargetID: "p1", Patch: map[string]any{}}},
			Confirmation:      RepairAutoStage,
		},
	}
}

func TestValidateRepairProposal_RequiredFields(t *testing.T) {
	base := baseRepairProposal()
	if err := ValidateRepairProposal(base); err != nil {
		t.Fatalf("baseline RepairProposal should validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*RepairProposal)
	}{
		{"bad apiVersion", func(rp *RepairProposal) { rp.APIVersion = "x" }},
		{"bad kind", func(rp *RepairProposal) { rp.Kind = "Something" }},
		{"missing metadata.id", func(rp *RepairProposal) { rp.Metadata = Metadata{} }},
		{"missing reportFingerprint", func(rp *RepairProposal) { rp.Spec.ReportFingerprint = "" }},
		{"malformed reportFingerprint", func(rp *RepairProposal) { rp.Spec.ReportFingerprint = "nope" }},
		{"unknown author.kind", func(rp *RepairProposal) { rp.Spec.Author = RepairAuthor{Kind: "robot"} }},
		{"llm author missing model", func(rp *RepairProposal) { rp.Spec.Author = RepairAuthor{Kind: "llm"} }},
		{"invalid ownership", func(rp *RepairProposal) { rp.Spec.Ownership = "nobody" }},
		{"empty changes", func(rp *RepairProposal) { rp.Spec.Changes = nil }},
		{"change bad targetKind", func(rp *RepairProposal) {
			rp.Spec.Changes = []RepairChange{{TargetKind: "Generation", TargetID: "g1"}}
		}},
		{"change missing targetId", func(rp *RepairProposal) {
			rp.Spec.Changes = []RepairChange{{TargetKind: "Profile"}}
		}},
		{"invalid confirmation", func(rp *RepairProposal) { rp.Spec.Confirmation = "MAYBE" }},
		{"prohibited confirmation", func(rp *RepairProposal) { rp.Spec.Confirmation = RepairProhibited }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rp := base
			rp.Spec.Changes = append([]RepairChange{}, base.Spec.Changes...)
			c.mutate(&rp)
			if err := ValidateRepairProposal(rp); err == nil {
				t.Errorf("expected an error for %s", c.name)
			}
		})
	}

	// A llm author WITH a model is fine.
	llm := base
	llm.Spec.Author = RepairAuthor{Kind: "llm", Model: "claude-sonnet-5"}
	if err := ValidateRepairProposal(llm); err != nil {
		t.Errorf("llm author with model should validate: %v", err)
	}
}

func baseEvidenceDoc() Evidence {
	return Evidence{
		APIVersion: SupportedAPIVersion,
		Kind:       "Evidence",
		Metadata:   Metadata{ID: "evidence:1"},
		Spec: EvidenceSpec{
			Subject:    EvidenceSubject{Concept: "instruction", LogicalID: "id1"},
			Level:      EvidenceLevelResolved,
			Guarantee:  GuaranteeReconciled,
			ObservedAt: "2026-07-16T00:00:00Z",
		},
	}
}

func TestValidateEvidence_RequiredFields(t *testing.T) {
	base := baseEvidenceDoc()
	if err := ValidateEvidence(base); err != nil {
		t.Fatalf("baseline Evidence should validate: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Evidence)
	}{
		{"bad apiVersion", func(e *Evidence) { e.APIVersion = "x" }},
		{"bad kind", func(e *Evidence) { e.Kind = "Something" }},
		{"missing metadata.id", func(e *Evidence) { e.Metadata = Metadata{} }},
		{"missing subject.concept", func(e *Evidence) { e.Spec.Subject.Concept = "" }},
		{"missing subject.logicalId", func(e *Evidence) { e.Spec.Subject.LogicalID = "" }},
		{"invalid level", func(e *Evidence) { e.Spec.Level = "E9" }},
		{"invalid guarantee", func(e *Evidence) { e.Spec.Guarantee = "MAYBE" }},
		{"missing observedAt", func(e *Evidence) { e.Spec.ObservedAt = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := base
			c.mutate(&e)
			if err := ValidateEvidence(e); err == nil {
				t.Errorf("expected an error for %s", c.name)
			}
		})
	}
}
