package knowledge

import "testing"

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in      string
		want    semver
		wantErr bool
	}{
		{"0.144.5", semver{0, 144, 5}, false},
		{"2.1.211", semver{2, 1, 211}, false},
		{"1.0.0-beta.1", semver{1, 0, 0}, false},
		{"1.0.0+build.5", semver{1, 0, 0}, false},
		{"  1.2.3  ", semver{1, 2, 3}, false},
		{"latest", semver{}, true},
		{"1.2", semver{}, true},
		{"1.2.3.4", semver{}, true},
		{"", semver{}, true},
		{"v1.2.3", semver{}, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := parseSemver(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseSemver(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			}
			if err == nil && got != c.want {
				t.Errorf("parseSemver(%q) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

func TestSemverCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"0.144.0", "0.144.5", -1},
		{"0.145.0", "0.144.5", 1},
	}
	for _, c := range cases {
		a, err := parseSemver(c.a)
		if err != nil {
			t.Fatal(err)
		}
		b, err := parseSemver(c.b)
		if err != nil {
			t.Fatal(err)
		}
		if got := a.compare(b); got != c.want {
			t.Errorf("%s.compare(%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestVersionSatisfiesRange(t *testing.T) {
	cases := []struct {
		name    string
		version string
		rng     string
		want    bool
		wantErr bool
	}{
		{"inside codex range", "0.144.5", ">=0.144.0 <0.145.0", true, false},
		{"lower bound inclusive", "0.144.0", ">=0.144.0 <0.145.0", true, false},
		{"upper bound exclusive", "0.145.0", ">=0.144.0 <0.145.0", false, false},
		{"below range", "0.143.9", ">=0.144.0 <0.145.0", false, false},
		{"at exclusive upper minus one patch", "0.144.999", ">=0.144.0 <0.145.0", true, false},
		{"inside claude range", "2.1.211", ">=2.1.0 <2.2.0", true, false},
		{"outside claude range major bump", "3.0.0", ">=2.1.0 <2.2.0", false, false},
		{"bare version is exact match, equal", "1.2.3", "1.2.3", true, false},
		{"bare version is exact match, unequal", "1.2.4", "1.2.3", false, false},
		{"explicit equals", "1.2.3", "=1.2.3", true, false},
		{"greater than", "1.2.4", ">1.2.3", true, false},
		{"greater than boundary excluded", "1.2.3", ">1.2.3", false, false},
		{"less than or equal boundary included", "1.2.3", "<=1.2.3", true, false},
		{"less than or equal below", "1.2.2", "<=1.2.3", true, false},
		{"less than or equal above", "1.2.4", "<=1.2.3", false, false},
		{"invalid version", "not-a-version", ">=0.1.0", false, true},
		{"invalid range", "1.0.0", "not-a-range", false, true},
		{"empty range", "1.0.0", "", false, true},
		{"floating latest as range", "1.0.0", "latest", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := VersionSatisfiesRange(c.version, c.rng)
			if (err != nil) != c.wantErr {
				t.Fatalf("VersionSatisfiesRange(%q, %q) error = %v, wantErr %v", c.version, c.rng, err, c.wantErr)
			}
			if err == nil && got != c.want {
				t.Errorf("VersionSatisfiesRange(%q, %q) = %v, want %v", c.version, c.rng, got, c.want)
			}
		})
	}
}
