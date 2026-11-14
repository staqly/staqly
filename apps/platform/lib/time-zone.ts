/** This browser's time zone (IANA name), UTC when it has none. Apart from the picker, so the shell needs no list. */
export const browserTimeZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
