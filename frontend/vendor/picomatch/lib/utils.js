/*global navigator*/
'use strict';

const {
  REGEX_BACKSLASH,
  REGEX_SPECIAL_CHARS,
  REGEX_SPECIAL_CHARS_GLOBAL
} = require('./constants');

exports.isObject = val => val !== null && typeof val === 'object' && !Array.isArray(val);
exports.hasRegexChars = str => REGEX_SPECIAL_CHARS.test(str);
exports.isRegexChar = str => str.length === 1 && exports.hasRegexChars(str);
exports.escapeRegex = str => str.replace(REGEX_SPECIAL_CHARS_GLOBAL, '\\$1');
exports.toPosixSlashes = str => str.replace(REGEX_BACKSLASH, '/');

exports.isWindows = () => {
  if (typeof navigator !== 'undefined' && navigator.platform) {
    const platform = navigator.platform.toLowerCase();
    return platform === 'win32' || platform === 'windows';
  }

  if (typeof process !== 'undefined' && process.platform) {
    return process.platform === 'win32';
  }

  return false;
};

exports.removeBackslashes = str => {
  const lineBreak = /[\r\n\u2028\u2029]/;
  const closesBracket = index => str[index] === ']' && str[index - 1] !== '\\';
  let output = '';
  let bracketEnd = 0;

  for (let index = 0; index < str.length; index++) {
    const char = str[index];
    if (char === '[') {
      // Keep the search cursor between openers so an unmatched bracket
      // cannot cause the same suffix to be scanned again.
      bracketEnd = Math.max(bracketEnd, index + 2);
      while (bracketEnd < str.length && !closesBracket(bracketEnd) &&
             !lineBreak.test(str[bracketEnd - 1])) {
        bracketEnd++;
      }
      if (closesBracket(bracketEnd)) {
        output += str.slice(index, bracketEnd + 1);
        index = bracketEnd;
        continue;
      }
    }
    if (char !== '\\' || index + 1 === str.length || lineBreak.test(str[index + 1])) {
      output += char;
    }
  }
  return output;
};

exports.escapeLast = (input, char, lastIdx) => {
  const idx = input.lastIndexOf(char, lastIdx);
  if (idx === -1) return input;
  if (input[idx - 1] === '\\') return exports.escapeLast(input, char, idx - 1);
  return `${input.slice(0, idx)}\\${input.slice(idx)}`;
};

exports.removePrefix = (input, state = {}) => {
  let output = input;
  if (output.startsWith('./')) {
    output = output.slice(2);
    state.prefix = './';
  }
  return output;
};

exports.wrapOutput = (input, state = {}, options = {}) => {
  const prepend = options.contains ? '' : '^';
  const append = options.contains ? '' : '$';

  let output = `${prepend}(?:${input})${append}`;
  if (state.negated === true) {
    output = `(?:^(?!${output}).*$)`;
  }
  return output;
};

exports.basename = (path, { windows } = {}) => {
  const segs = path.split(windows ? /[\\/]/ : '/');
  const last = segs[segs.length - 1];

  if (last === '') {
    return segs[segs.length - 2];
  }

  return last;
};
