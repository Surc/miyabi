import { readFile } from 'node:fs/promises';
import { start } from './server.mjs';

if (process.argv.length !== 3) throw new Error('Usage: node start.mjs /absolute/path/to/browser-service.json');
await start(JSON.parse(await readFile(process.argv[2], 'utf8')));
