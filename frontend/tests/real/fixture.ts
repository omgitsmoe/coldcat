export const fixture = {
  label: 'Archive <alpha>&',
  serial: 'SERIAL-α',
  notes: 'Offline inventory, not live usage <b>',
  capacity: '9007199254740993',
  directory: 'foo/É e\u0301\\x ?#% &<tag>',
  longDirectory: 'long-' + '界'.repeat(80),
  sharedHash: 'ab'.repeat(32),
  differentHash: 'cd'.repeat(32),
  oldHash: 'ef'.repeat(32),
  unknownHash: '12'.repeat(32),
  zeroHash: '34'.repeat(32),
  oldCapture: '2020-01-02T03:04:05.123456789Z',
  middleCapture: '2021-02-03T04:05:06Z',
  currentCapture: '2022-03-04T05:06:07.987654321Z',
  mtime: '2000-01-01T00:00:00Z',
};

export const reportPath = `${fixture.directory}/report.txt`;

function record(path: string, hash: string, size: string, mtime = '') {
  return `${mtime},${size},sha256,${hash} ${path}\n`;
}

export const inventories = [
  {
    name: 'alpha-current',
    label: fixture.label,
    capturedAt: fixture.currentCapture,
    records:
      record(reportPath, fixture.sharedHash, '7', '946684800') +
      record('foo/copy.txt', fixture.sharedHash, '7') +
      record('foo/child/alias.txt', fixture.sharedHash, '7') +
      record('foo/unknown.txt', fixture.unknownHash, '') +
      record('foo/zero.txt', fixture.zeroHash, '0') +
      record('foobar/report.txt', fixture.differentHash, '11') +
      record(`${fixture.longDirectory}/long-report.txt`, fixture.differentHash, '11') +
      record('短', fixture.zeroHash, '0'),
  },
  {
    name: 'alpha-old',
    label: fixture.label,
    capturedAt: fixture.oldCapture,
    records:
      record(reportPath, fixture.sharedHash, '7', '946684800') +
      record('foo/retired.txt', fixture.oldHash, '5'),
  },
  {
    name: 'alpha-middle',
    label: fixture.label,
    capturedAt: fixture.middleCapture,
    records: record(reportPath, fixture.sharedHash, '7', '946684800'),
  },
  {
    name: 'beta-current',
    label: 'Backup β',
    capturedAt: fixture.currentCapture,
    records: record('renamed/alias.txt', fixture.sharedHash, '7'),
  },
  {
    name: 'gamma-current',
    label: 'Third γ',
    capturedAt: fixture.currentCapture,
    records: record('other/report.txt', fixture.sharedHash, '7'),
  },
];
