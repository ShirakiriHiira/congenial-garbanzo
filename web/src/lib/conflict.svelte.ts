/**
 * Shared 409 flow: never auto-retry or merge. The page opens the dialog, reloads
 * the current server entity for display, and only a fresh user submit may write.
 */
export class ConflictFlow<T> {
  open = $state(false);
  loading = $state(false);
  failed = $state(false);
  current = $state<T | null>(null);
  #reload: () => Promise<T>;

  constructor(reload: () => Promise<T>) {
    this.#reload = reload;
  }

  get changed(): boolean {
    return this.failed || this.current !== null;
  }

  /** Reloads the authoritative entity; a failed reload keeps writes blocked. */
  async present(): Promise<void> {
    this.open = true;
    this.loading = true;
    this.failed = false;
    try {
      this.current = await this.#reload();
    } catch {
      this.current = null;
      this.failed = true;
    } finally {
      this.loading = false;
    }
  }

  async retry(): Promise<void> {
    if (this.loading) return;
    await this.present();
  }

  /** Caller has based its draft on the reloaded entity and may submit again. */
  dismiss(): void {
    this.open = false;
    this.loading = false;
  }
}
