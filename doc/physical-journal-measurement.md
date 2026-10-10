# Physical journal measurement validation

**Scrapped by user decision on 2026-10-10.** Journal file-size measurements, including
existing sampled maxima, are sufficient for backend acceptance evidence. The effort of
exact physical allocation accounting is disproportionate to its practical usefulness.
No further completion probes, allocation-ledger validation or tracing-tool installation
is required. This document and the lifecycle harness preserve the acquired information;
the investigation and commands below are historical reference, not outstanding tasks.
No exact physical peak was established, and this decision does not approve performance
budgets or the final backend gate.

## Host and accounting boundary

The inspected host runs Linux `7.2.6-arch2-1`. `/home` is ext4 on `/dev/nvme0n1p6`,
with 4096-byte blocks, 32768 blocks per group, and no `bigalloc` feature listed.
The repository resides on a different `fuseblk` filesystem. Test files must therefore
be placed explicitly on `/home`, not in the repository or the tmpfs `/tmp`.

The former proposed primary boundary was filesystem blocks attributable to SQLite journals,
with delayed-allocation reservations and allocator preallocation reported separately.
Accounting for simultaneous catalog/sidecar/temp storage and transaction-pending frees
would have needed an agreed definition. SSD-internal allocation was outside this boundary.
An ADR was proposed for these semantics; none was created. The physical boundary is no
longer an acceptance requirement.

## Source inspection

Upstream stable `v7.2.6` sources were inspected:

- https://github.com/gregkh/linux/blob/v7.2.6/fs/ext4/mballoc.c
- https://github.com/gregkh/linux/blob/v7.2.6/fs/ext4/inode.c

Arch patch equivalence remains to be checked before claiming exact running-kernel semantics.
The upstream call sites already rule out a simple tracepoint sum:

- `ext4_allocate_blocks` is emitted at the allocation function exit, including error
  paths with zero length. Successful nonzero allocations need separate handling.
- `ext4_free_blocks` is emitted before `ext4_mb_clear_bb` performs freeing.
- `ext4_mballoc_free` is also emitted before the bitmap update; it is not a second free
  to subtract and is not a completion event.
- Ordered-data and metadata freeing can enter `ext4_mb_free_metadata`; allocator reuse
  is delayed until the commit-driven `ext4_free_data_in_buddy` path.
- `ext4_getattr` adds delayed-allocation reservations to reported `st_blocks`. Consequently
  even `fstat().st_blocks * 512` is not independent proof of physical extent allocation.

Physical accounting must resolve completed frees, pending reuse, allocator preallocation,
metadata attribution, event ordering and loss before producing a peak. Extent-status cache
events are not physical allocation events. No polling or logical VFS replacement is used.

## Lifecycle fixture

Build and check from the repository root:

```sh
go test ./scripts/validate-physical-journal
go build -o /tmp/opencode/validate-physical-journal ./scripts/validate-physical-journal
/tmp/opencode/validate-physical-journal -dir /home/m -trace-marker /dev/null
```

The explicit `/dev/null` marker is a smoke check only. It does not collect a trace.
The fixture requires ext4 with 4096-byte blocks and creates its own temporary directory.
It exercises sparse extension, buffered writes followed by fsync, preallocation, truncation,
simultaneously live files, open-unlinked writes, and rapid inode lifecycles. JSON checkpoints
contain device/inode/link identity, logical bytes and `stat_blocks_bytes`; the latter is a
filesystem-reported checkpoint, not a physical-peak metric. Checkpoints are also written
to the selected trace marker. Files are removed, then `syncfs` finishes the filesystem
durability tail. This sync applies to the parent filesystem, not only the fixture.

An unprivileged host smoke run completed all 46 checkpoints. Sparse extension reported
zero blocks; the synced sparse write reported 1 MiB; truncate reported zero; preallocation
reported 4 MiB. An open-unlinked file retained the same inode and reported 2 MiB after
the second write. The host immediately reused that inode for another file, and rapid
cycles reused an inode repeatedly. Device/inode alone is therefore not a durable identity:
allocation/free lifecycle events must delimit each lifetime. This smoke run validates
fixture behavior only, not instrumentation or allocation accounting.

## Privileged capture

`trace-cmd` 3.4.0 is installed on the inspected host. Run this after building the binary;
it is compatible with Fish and Bash. The instance name is reserved for this validation.
Use a new output filename for each capture.

```sh
sudo trace-cmd record -B coldcat-physical-validation -C global -b 16384 \
    -e ext4 -e jbd2 \
    -o /tmp/opencode/coldcat-physical-validation.dat \
    /tmp/opencode/validate-physical-journal -dir /home/m \
    -trace-marker /sys/kernel/tracing/instances/coldcat-physical-validation/trace_marker
```

The global clock provides a common timestamp domain; it does not make events atomic
with underlying filesystem operations. Capture all filesystem threads, not only the
fixture PID, because background writeback/reclamation must be observed. Trace output
resides outside the measured filesystem. No project catalog is opened.

Export the report, preserving stderr and the record command's terminal output for loss
diagnostics:

```sh
sudo trace-cmd report -i /tmp/opencode/coldcat-physical-validation.dat \
    > /tmp/opencode/coldcat-physical-validation.txt \
    2> /tmp/opencode/coldcat-physical-validation-report-errors.txt
```

Before import runs, confirm all fixture lifetimes and markers are present, reject any
lost events, and independently reconcile stable extents/reservations and completed frees.
Additional probes or an instrumented kernel may be required. This capture does not yet
include those completion probes and must not be treated as an approved peak measurement.

## First privileged capture

The host capture on 2026-10-10 produced
`/tmp/opencode/coldcat-physical-validation.dat`, SHA-256
`f92f76e504ea616cf7d9faddeb316bc42167a87e6e837bd873b100d54e324207`.
The named instance contains 36,236 events, all 46 JSON checkpoints and the final
`cleanup-syncfs-complete` marker. Empty top-level buffers are expected because events
were enabled in the named instance. Report stderr is empty and no lost-event notices
were found. Saved buffer statistics report zero overruns, commit overruns and dropped
events, but nonzero remaining entries on four CPUs. Inspection of upstream trace-cmd
3.4 (`tracecmd/trace-record.c`, commit `3ce20923b3efa60d417da7acc8a327615fbd1419`)
resolves the timing: `record_stats()` precedes `wait_threads()`, and the recorder performs
a final drain before returning. These saved entries are therefore not by themselves
evidence of missing records. The fixture markers and all 21 inode lifetimes are present;
this still does not establish physical-allocation instrumentation completeness.

Across the fixture's five device/inode keys, delimited by lifecycle events, the report
contains 21 inode allocations, 21 inode frees, 22 block allocations, 22 free requests,
22 allocator free requests, one inode preallocation creation/release pair, 5376
delayed-allocation reservation events and 21 reservation-update events. No reservation
release events were observed for these files.

Diagnostic allocation-minus-free-request totals match reported blocks at every synced
checkpoint and return to zero for all keys. These are request-level reconciliation,
not completed-free or physical-peak evidence. At `sparse-written-buffered`, `st_blocks`
reports 256 blocks while no allocation event has occurred; after fsync the allocation
trace accounts for 256 blocks. This demonstrates why reported blocks alone cannot
prove physical allocation. Reservation-update events likewise report the pre-update
reservation balance: upstream emits them before decrementing `i_reserved_data_blocks`.

Some formatted JBD2 events contain `[FAILED TO PARSE]` even though report stderr is empty.
Use raw field export with full timestamp precision for further analysis:

```sh
trace-cmd report -R -t -i /tmp/opencode/coldcat-physical-validation.dat \
    > /tmp/opencode/coldcat-physical-validation-raw.txt
trace-cmd report --stat -i /tmp/opencode/coldcat-physical-validation.dat
```

The capture file is world-readable on this host; report export does not need sudo.
The installed kernel image exposes `ext4_mb_mark_context`, `ext4_process_freed_data`
and `mb_free_blocks` symbols. `ext4_free_data_in_buddy` and `ext4_mb_clear_bb` are not
standalone symbols in that image. BTF and kernel probe support are enabled. The next
feasibility step is to inspect available typed/function probes for completed bitmap
changes and buddy release, preserving physical-range ownership across inode reuse.
