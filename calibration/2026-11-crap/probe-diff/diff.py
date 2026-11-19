#!/usr/bin/env python3
"""Compare the native record with the probe, function by function.

usage: diff.py <results.json> <probe dump>   (python 3, standard library only)

Joins on repo, file and function name (a receiver is written Recv.Name). It
prints the functions only one side has, every function whose complexity
differs, and every function that is over a cap on one side and not on the
other, with the figures that decide it. A native complexity that is not between
the probe's discounted figure and the raw gocyclo count is reported as a
surprise: the native count discounts a subset of the guards the probe did.
"""
import collections, json, sys

def crap(c, cov): return c * c * (1 - cov) ** 3 + c
def frac(a, b): return 1.0 if b == 0 else a / b

nat = json.load(open(sys.argv[1]))
N = collections.defaultdict(list)
for r in nat["repos"]:
    for f in sorted(r["functions"], key=lambda f: f["key"]):
        name = f["key"].split("@")[0].rsplit("/", 1)[-1].split(".", 1)[1]
        N[(r["repo"], f["file"], name)].append(f)
    for f in r.get("not_measured") or []:
        sys.exit("a not-measured function: this comparison assumes every function is measured")
P = collections.defaultdict(list)
for line in open(sys.argv[2]):
    repo, file, name, ln, ccd, st, cov, cc, g = line.rstrip("\n").split("\t")
    P[(repo, file, name)].append(dict(line=int(ln), ccd=int(ccd), crap=crap(int(ccd), frac(int(cov), int(st))), raw=int(cc), guards=int(g)))

only_n = [(k, f) for k in N if k not in P for f in N[k]]
only_p = [(k, f) for k in P if k not in N for f in P[k]]
print(f"functions: native {sum(map(len, N.values()))}, probe {sum(map(len, P.values()))}")
print(f"only native ({len(only_n)}):")
for k, f in only_n: print(f"  {k[0]} {k[1]} {k[2]}  comp {f['comp']}  CRAP {f['crap']:.2f}")
print(f"only probe ({len(only_p)}):")
for k, f in only_p: print(f"  {k[0]} {k[1]} {k[2]}  cc {f['ccd']}")

pairs = []
for k in N:
    if k in P and len(N[k]) == len(P[k]):
        for n, p in zip(N[k], sorted(P[k], key=lambda x: x["line"])):
            pairs.append((k, n, p))
    elif k in P:
        print("group size differs:", k, len(N[k]), len(P[k]))

print("\ncomplexity differs (native comp, probe discounted cc, raw cc, guards the probe discounted):")
diff = [(k, n, p) for k, n, p in pairs if n["comp"] != p["ccd"]]
for k, n, p in diff: print(f"  {k[0]:7} {k[1]} {k[2]}: {n['comp']} vs {p['ccd']} (raw {p['raw']}, {p['guards']} guards)")
surprise = [(k, n, p) for k, n, p in diff if not (p["ccd"] <= n["comp"] <= p["raw"])]
print(f"  {len(diff)} functions; {len(surprise)} with a native complexity outside [probe discounted, raw]")
print("  by repo:", dict(collections.Counter(k[0] for k, _, _ in diff)))

for c in (6, 8, 10):
    over = [(k, n, p) for k, n, p in pairs if (n["crap"] > c) != (p["crap"] > c + 1e-9)]
    alone = [(k, n, p) for k, n, p in pairs if (n["comp"] > c) != (p["ccd"] > c)]
    print(f"\ncap {c}: over the cap on one side only: {len(over)}; complexity alone above it on one side only: {len(alone)}")
    for k, n, p in over:
        print(f"  over   {k[0]:7} {k[1]} {k[2]}: native comp {n['comp']} CRAP {n['crap']:.2f}, probe cc {p['ccd']} CRAP {p['crap']:.2f}")
    for k, n, p in alone:
        print(f"  alone  {k[0]:7} {k[1]} {k[2]}: native comp {n['comp']}, probe cc {p['ccd']}")
