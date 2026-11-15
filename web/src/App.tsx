import { useEffect } from 'react';
import { Icon } from './components/Icon';
import { ErrorView } from './components/ui';
import { useI18n } from './i18n';
import { href, parse, useRoutePath } from './router';
import { DiffScreen } from './screens/DiffScreen';
import { DocScreen } from './screens/DocScreen';
import { TaskList } from './screens/TaskList';
import { TaskScreen } from './screens/TaskScreen';
import { TodoList, TodoScreen } from './screens/Todos';
import { useTheme } from './theme';
import { FAKE } from './api';

function Header({ section }: { section: 'tasks' | 'todos' }) {
  const { t, lang, setLang } = useI18n();
  const [theme, cycleTheme] = useTheme();
  const themeIcon = theme === 'light' ? 'sun' : theme === 'dark' ? 'moon' : 'system';
  return (
    <header className="topbar">
      <a className="brand" href={href('/')} aria-label={t('app.name')}>
        <span className="brand-mark" aria-hidden="true">
          <Icon name="check" size={14} />
        </span>
        <span className="brand-name">{t('app.name')}</span>
      </a>
      <nav className="topnav">
        <a href={href('/')} className={section === 'tasks' ? 'on' : ''} aria-current={section === 'tasks' ? 'page' : undefined}>
          {t('nav.tasks')}
        </a>
        <a href={href('/todos')} className={section === 'todos' ? 'on' : ''} aria-current={section === 'todos' ? 'page' : undefined}>
          {t('nav.todos')}
        </a>
      </nav>
      {FAKE && (
        <span className="mock-flag" title={t('app.mockup')}>
          {t('app.mockup')}
        </span>
      )}
      <div className="topbar-actions">
        <button type="button" className="btn btn-ghost btn-sm" onClick={() => setLang(lang === 'en' ? 'ru' : 'en')} title={t('lang.switch')} aria-label={t('lang.switch')}>
          <Icon name="globe" size={15} />
          <span>{lang === 'en' ? 'RU' : 'EN'}</span>
        </button>
        <button type="button" className="btn btn-ghost btn-sm btn-icon" onClick={cycleTheme} title={t(`theme.${theme}`)} aria-label={t(`theme.${theme}`)}>
          <Icon name={themeIcon} size={15} />
        </button>
      </div>
    </header>
  );
}

export function App() {
  const path = useRoutePath();
  const route = parse(path);
  const routeKey = route.name === 'list' || route.name === 'todos' ? route.name : path.split('?')[0];
  useEffect(() => {
    window.scrollTo(0, 0);
  }, [routeKey]);

  let screen;
  switch (route.name) {
    case 'list':
      screen = <TaskList query={route.query} />;
      break;
    case 'todos':
      screen = <TodoList query={route.query} />;
      break;
    case 'todo':
      screen = <TodoScreen key={route.id} id={route.id} />;
      break;
    case 'task':
      screen = <TaskScreen key={route.id} id={route.id} query={route.query} />;
      break;
    case 'doc':
      screen = <DocScreen key={`${route.id}/${route.slug}/${route.rev ?? ''}`} id={route.id} slug={route.slug} rev={route.rev} query={route.query} />;
      break;
    case 'diff':
      screen = <DiffScreen id={route.id} slug={route.slug} from={route.from} to={route.to} />;
      break;
    default:
      screen = <ErrorView error={new Error(`no page at ${route.path}`)} />;
  }
  return (
    <>
      <Header section={route.name === 'todos' || route.name === 'todo' ? 'todos' : 'tasks'} />
      <main>{screen}</main>
    </>
  );
}
