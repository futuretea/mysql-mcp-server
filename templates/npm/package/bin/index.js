#!/usr/bin/env node

const childProcess = require('child_process');

const architecture = {x64: 'amd64', arm64: 'arm64'}[process.arch];
const platform = process.platform === 'win32' ? 'windows' : process.platform;
const suffix = process.platform === 'win32' ? '.exe' : '';

if (!architecture) {
  throw new Error(`Unsupported architecture: ${process.arch}`);
}

const packageName = `mcp-template-npm-package-placeholder-${platform}-${architecture}`;
const binaryName = 'mcp-template-binary-placeholder';

const resolveBinaryPath = () => {
  try {
    return require.resolve(`${packageName}/bin/${binaryName}${suffix}`);
  } catch {
    throw new Error(`Could not resolve a binary for ${process.platform}/${process.arch}`);
  }
};

const child = childProcess.spawn(resolveBinaryPath(), ['mcp', ...process.argv.slice(2)], {
  stdio: 'inherit',
});

for (const signal of ['SIGTERM', 'SIGINT', 'SIGHUP']) {
  process.on(signal, () => {
    if (!child.killed) child.kill(signal);
  });
}

child.on('close', (code, signal) => {
  if (signal) process.exit(128 + (signal === 'SIGTERM' ? 15 : signal === 'SIGINT' ? 2 : 1));
  process.exit(code || 0);
});
