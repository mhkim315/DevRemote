const { randomBytes } = require('crypto');
exports.getRandomBytesAsync = async (size) => new Uint8Array(randomBytes(size));
exports.digestStringAsync = async () => '';
