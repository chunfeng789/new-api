/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// Upload the hashed build output (dist/static) to Tencent COS and purge the
// CDN path, so production pages built with ASSET_PREFIX load their JS/CSS/
// fonts from the CDN. index.html and public/ files are not uploaded; they are
// still served by the Go binary.
//
// Environment:
//   TENCENT_SECRET_ID / TENCENT_SECRET_KEY  required; skipped when absent
//   ASSET_PREFIX      CDN origin + key prefix, e.g. https://cdn.example.com/new-api/
//   CDN_COS_BUCKET    COS bucket name (default gpttalk-static-1318401827)
//   CDN_COS_REGION    COS region (default ap-shanghai)
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import COS from 'cos-nodejs-sdk-v5'
import { cdn } from 'tencentcloud-sdk-nodejs-cdn'

const secretId = process.env.TENCENT_SECRET_ID
const secretKey = process.env.TENCENT_SECRET_KEY
const assetPrefix = process.env.ASSET_PREFIX

if (!secretId || !secretKey) {
  console.log(
    '[upload-cdn] TENCENT_SECRET_ID/TENCENT_SECRET_KEY not set, skipping upload'
  )
  process.exit(0)
}
if (!assetPrefix || !/^https?:\/\//.test(assetPrefix)) {
  console.error(
    '[upload-cdn] ASSET_PREFIX must be an absolute URL, got:',
    assetPrefix
  )
  process.exit(1)
}

const bucket = process.env.CDN_COS_BUCKET || 'gpttalk-static-1318401827'
const region = process.env.CDN_COS_REGION || 'ap-shanghai'
// Object keys mirror the URL path under the CDN origin so `${ASSET_PREFIX}static/js/x.js`
// resolves to key `<prefix>/static/js/x.js`.
const keyPrefix = new URL(assetPrefix).pathname.replace(/^\/+/, '')
const purgePath = `${assetPrefix.replace(/\/+$/, '')}/static/`

const distDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../dist'
)
const staticDir = path.join(distDir, 'static')
if (!fs.existsSync(staticDir)) {
  console.error('[upload-cdn] build output not found:', staticDir)
  process.exit(1)
}

const files = fs
  .readdirSync(staticDir, { recursive: true, withFileTypes: true })
  .filter((entry) => entry.isFile())
  .map((entry) => path.join(entry.parentPath, entry.name))

const cos = new COS({ SecretId: secretId, SecretKey: secretKey })
const concurrency = 16
let nextIndex = 0

async function uploadWorker() {
  while (nextIndex < files.length) {
    const file = files[nextIndex++]
    const key =
      keyPrefix + path.relative(distDir, file).split(path.sep).join('/')
    await cos.putObject({
      Bucket: bucket,
      Region: region,
      Key: key,
      Body: fs.createReadStream(file),
      CacheControl: 'public, max-age=31536000, immutable',
    })
    console.log(`[upload-cdn] ${key}`)
  }
}

await Promise.all(Array.from({ length: concurrency }, uploadWorker))
console.log(
  `[upload-cdn] uploaded ${files.length} files to ${bucket}/${keyPrefix}static/`
)

const cdnClient = new cdn.v20180606.Client({
  credential: { secretId, secretKey },
})
const purge = await cdnClient.PurgePathCache({
  Paths: [purgePath],
  FlushType: 'flush',
})
console.log(`[upload-cdn] purged ${purgePath} (task ${purge.TaskId})`)
