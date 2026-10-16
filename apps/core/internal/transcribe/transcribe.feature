Feature: Audio transcription muscle
  As the transcoder worker (and any product reusing this lib)
  I want to turn an audio source into timed segments
  So that callers get a transcript without owning ffmpeg/STT orchestration

  The lib is pure: it takes a Source in and returns segments out. It never
  touches NATS, a database, or any product concern.

  Scenario: Short audio is transcribed in a single shot
    Given a stub STT provider that echoes the file name
    And an audio source under the single-shot size and duration limits
    When the transcriber transcribes the source
    Then it returns a single segment at offset 0

  Scenario: Long audio is split into chunks and transcribed in parallel
    Given a stub STT provider that echoes the file name
    And an audio source longer than the single-shot duration limit
    When the transcriber transcribes the source
    Then it returns one segment per chunk
    And each segment is tagged with its chunk start offset

  Scenario: A minority of failing chunks degrades gracefully
    Given a stub STT provider that fails exactly one chunk out of four
    And an audio source longer than the single-shot duration limit
    When the transcriber transcribes the source
    Then the surviving chunks are kept
    And the failed chunk carries an "unavailable" marker

  Scenario: A majority of failing chunks fails hard
    Given a stub STT provider that fails three chunks out of four
    And an audio source longer than the single-shot duration limit
    When the transcriber transcribes the source
    Then transcription fails with an error

  Scenario: A rate-limited provider is retried within a widened budget
    Given a stub STT provider that returns 429 four times then succeeds
    And an audio source longer than the single-shot duration limit
    When the transcriber transcribes the source
    Then it eventually returns the successful transcript
