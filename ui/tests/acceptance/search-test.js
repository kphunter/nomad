/* eslint-disable ember-a11y-testing/a11y-audit-called */ // TODO
import { module, test } from 'qunit';
import { triggerEvent, visit } from '@ember/test-helpers';
import { setupApplicationTest } from 'ember-qunit';
import { setupMirage } from 'ember-cli-mirage/test-support';
import Layout from 'nomad-ui/tests/pages/layout';
import JobsList from 'nomad-ui/tests/pages/jobs/list';
import { selectSearch } from 'ember-power-select/test-support';

module('Acceptance | search', function(hooks) {
  setupApplicationTest(hooks);
  setupMirage(hooks);

  test('server-side truncation is indicated in the group label', async function(assert) {
    server.create('node', { name: 'xyz' });

    for (let i = 0; i < 21; i++) {
      server.create('job', { id: `job-${i}`, namespaceId: 'default' });
    }

    await visit('/');

    await selectSearch(Layout.navbar.search.scope, 'job');

    Layout.navbar.search.as(search => {
      search.groups[0].as(jobs => {
        assert.equal(jobs.name, 'Jobs (showing 10 of 20+)');
      });
    });
  });

  test('clicking the search field starts search immediately', async function(assert) {
    await visit('/');

    assert.notOk(Layout.navbar.search.field.isPresent);

    await Layout.navbar.search.click();

    assert.ok(Layout.navbar.search.field.isPresent);
  });

  test('pressing slash starts a search', async function(assert) {
    await visit('/');

    assert.notOk(Layout.navbar.search.field.isPresent);

    await triggerEvent('.page-layout', 'keydown', {
      keyCode: 191, // slash
    });

    assert.ok(Layout.navbar.search.field.isPresent);
  });

  test('pressing slash when an input element is focused does not start a search', async function(assert) {
    server.create('node');
    server.create('job');

    await visit('/');

    assert.notOk(Layout.navbar.search.field.isPresent);

    await JobsList.search.click();
    await JobsList.search.keydown({ keyCode: 191 });

    assert.notOk(Layout.navbar.search.field.isPresent);
  });
});
