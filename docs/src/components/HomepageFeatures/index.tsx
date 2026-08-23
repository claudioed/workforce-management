import type {ReactNode} from 'react';
import clsx from 'clsx';
import Link from '@docusaurus/Link';
import Heading from '@theme/Heading';
import styles from './styles.module.css';

type FeatureItem = {
  title: string;
  to: string;
  description: ReactNode;
};

const FeatureList: FeatureItem[] = [
  {
    title: 'Human-Committed Shift Plans',
    to: '/docs/business-context/planning-horizons',
    description: (
      <>
        The software proposes — <code>heads = ceil(charge ÷ rate)</code> — and a
        human commits. A <code>ShiftPlan</code> is one building&rsquo;s split of
        headcount across process paths, validated as one atomic decision:
        planned heads never exceed installed stations.
      </>
    ),
  },
  {
    title: 'Certification-Gated Assignment',
    to: '/docs/ddd/invariants',
    description: (
      <>
        An associate untrained on a path cannot be put on it, and cannot hold two
        active assignments at once. The second rule is structural, not checked:
        the aggregate is keyed by associate and has exactly one field to hold an
        active interval in.
      </>
    ),
  },
  {
    title: 'Stops at the Path Boundary',
    to: '/docs/business-context/path-boundary',
    description: (
      <>
        There is no <code>taskId</code> in this service. Labor assignment moves
        over minutes; task dispatch moves over seconds. Keeping the seam at
        &ldquo;this associate is on this path&rdquo; lets each side change
        without touching the other.
      </>
    ),
  },
  {
    title: 'A Flag, Not a Decision',
    to: '/docs/ddd/domain-events',
    description: (
      <>
        When a path falls below its committed heads, this context raises{' '}
        <code>PathUnderstaffed</code> and stops. It surfaces the gap; a
        supervisor chooses the response and records it. Making the labor picture
        legible is the job.
      </>
    ),
  },
];

function Feature({title, to, description}: FeatureItem) {
  return (
    <div className={clsx('col col--6')}>
      <Link to={to} className={styles.featureCard}>
        <Heading as="h3" className={styles.featureTitle}>
          {title}
        </Heading>
        <p className={styles.featureBody}>{description}</p>
      </Link>
    </div>
  );
}

export default function HomepageFeatures(): ReactNode {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}
