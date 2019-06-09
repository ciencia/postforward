v1.2.0-ciencia / 2019-06-09
===================

  * Remove the "From:" header from original mail (postfix seems to obey it
    even if wrong)
  * Add -F parameter to sendmail, to get a nicer sender name instead of
    "nobody", when removing the From: header

v1.1.1 / 2019-02-22
===================

  * Intelligently guess correct line endings
  * Prevent insertion of additional linebreak in message body

v1.1.0 / 2017-10-28
===================

  * Get hostname from postfix "myhostname" value
  * Allow setting custom $PATH using --path argument
  * Makefile cleanups
  * Add debian build target

v1.0.1 / 2017-09-04
===================

  * Don't treat "." as end of message

v1.0.0 / 2017-09-04
===================

  * Initial commit
