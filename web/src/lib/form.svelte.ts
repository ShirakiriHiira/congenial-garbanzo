/**
 * Guards against duplicate writes and keeps drafts on failure. Callers own the
 * draft values; this only tracks in-flight state and the last error.
 */
export class Submitter {
  busy = $state(false);
  error = $state<unknown>(null);

  async run(task: () => Promise<void>): Promise<boolean> {
    if (this.busy) return false;
    this.busy = true;
    this.error = null;
    try {
      await task();
      return true;
    } catch (caught) {
      this.error = caught;
      return false;
    } finally {
      this.busy = false;
    }
  }

  clear(): void {
    this.error = null;
  }
}
