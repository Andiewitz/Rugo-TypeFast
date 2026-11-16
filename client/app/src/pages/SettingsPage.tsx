import './SettingsPage.css'

export function SettingsPage() {
  return (
    <main className="activity">
      <section className="activity-card info-card">
        <h1>Settings</h1>
        <dl className="build-version">
          <dt>Build Version</dt>
          <dd>{__APP_VERSION__}</dd>
        </dl>
      </section>
    </main>
  )
}
